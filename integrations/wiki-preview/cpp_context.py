# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 alibaba/open-code-review Contributors

"""Build complete, symbol-addressed C++ function inputs from frozen Git objects."""

import hashlib
from pathlib import PurePosixPath
import re
import subprocess


def parse_functions(path, text, parser):
    data = text.encode("utf-8")
    normalized = re.sub(rb"(?m)^(\s*)(__(?:TRY|CATCH)__)(\s*)$",
                        lambda match: match[1] + b" " * len(match[2]) + match[3], data)
    result = []
    for node in walk(parser.parse(normalized).root_node):
        if node.type != "function_definition" or function_declarator(node) is None:
            continue
        key, name = function_key(data, node, function_declarator(node))
        calls, allocations = [], []
        for call in walk(node):
            if call.type == "call_expression":
                target = call.child_by_field_name("function")
                calls.append({"expression": text_at(data, target), "line": call.start_point.row + 1})
            elif call.type == "new_expression":
                target = call.child_by_field_name("type")
                if target is not None:
                    allocations.append(text_at(data, target))
        result.append({"id": "src-" + hashlib.sha256((path + ":" + key).encode()).hexdigest()[:16],
                       "key": key, "name": name, "reference": reference(path, node),
                       "source": text_at(data, node), "calls": calls, "allocations": allocations,
                       "parse_error": node.has_error})
    return result


def walk(node):
    yield node
    for child in node.named_children:
        yield from walk(child)


def source_text(repo, commit, path):
    path = PurePosixPath(path)
    if path.is_absolute() or ".." in path.parts or ":" in str(path):
        raise ValueError("Source paths must be repository-relative")
    result = subprocess.run(["git", "-C", str(repo), "show", commit + ":" + str(path)], capture_output=True, check=True)
    try:
        text = result.stdout.decode("utf-8-sig")
    except UnicodeDecodeError:
        text = result.stdout.decode("gb18030")
    return text


def text_at(data, node):
    return data[node.start_byte:node.end_byte].decode("utf-8")


def reference(path, node):
    return {"file_path": path, "start_line": node.start_point.row + 1, "end_line": node.end_point.row + 1}


def enclosing_names(node):
    names = []
    parent = node.parent
    while parent:
        if parent.type in {"namespace_definition", "class_specifier", "struct_specifier"}:
            name = parent.child_by_field_name("name")
            if name:
                names.append(name.text.decode("utf-8"))
        parent = parent.parent
    return list(reversed(names))


def function_declarator(node):
    node = node.child_by_field_name("declarator")
    while node is not None:
        if node.type == "function_declarator":
            return node
        child = node.child_by_field_name("declarator")
        if child is None and node.type in {"reference_declarator", "pointer_declarator"}:
            child = next((n for n in node.named_children if n.type.endswith("declarator")), None)
        node = child
    return None


def parameter_type(data, node):
    end = node.end_byte
    default = node.child_by_field_name("default_value")
    if default:
        end = data.rfind(b"=", node.start_byte, default.start_byte)
    declarator = node.child_by_field_name("declarator")
    while declarator is not None and declarator.type not in {"identifier", "field_identifier"}:
        declarator = declarator.child_by_field_name("declarator")
    value = data[node.start_byte:end]
    if declarator is not None:
        start = declarator.start_byte - node.start_byte
        stop = declarator.end_byte - node.start_byte
        value = value[:start] + value[stop:]
    return re.sub(r"\s+", "", value.decode("utf-8"))


def function_key(data, node, decl):
    name = text_at(data, decl.child_by_field_name("declarator"))
    prefix = "::".join(enclosing_names(node))
    if prefix and not name.startswith(prefix + "::"):
        name = prefix + "::" + name
    parameters = decl.child_by_field_name("parameters")
    types = [parameter_type(data, p) for p in parameters.named_children if p.type != "comment"]
    qualifiers = [text_at(data, n) for n in decl.named_children if n.type in {"type_qualifier", "ref_qualifier"}]
    return name + "(" + ",".join(types) + ")" + "".join(qualifiers), name


def load_class(repo, commit, scope, class_name, header, implementations):
    from tree_sitter import Language, Parser
    import tree_sitter_cpp

    paths = list(dict.fromkeys([header, *implementations]))
    if not implementations or any(not path.startswith(scope + "/") for path in paths):
        raise ValueError("A pilot needs an explicit header and implementation inside its scope")
    parser = Parser(Language(tree_sitter_cpp.language()))
    sources = {path: source_text(repo, commit, path) for path in paths}
    encoded = {path: source.encode("utf-8") for path, source in sources.items()}
    parse_inputs = {path: re.sub(rb"(?m)^(\s*)(__(?:TRY|CATCH)__)(\s*)$",
                                lambda match: match[1] + b" " * len(match[2]) + match[3], data)
                    for path, data in encoded.items()}
    trees = {path: parser.parse(data) for path, data in parse_inputs.items()}
    classes = [n for n in walk(trees[header].root_node) if n.type in {"class_specifier", "struct_specifier"}
               and n.child_by_field_name("name") is not None
               and "::".join([*enclosing_names(n), text_at(encoded[header], n.child_by_field_name("name"))]) == class_name]
    if len(classes) != 1:
        raise ValueError("The requested class must resolve to one declaration")
    class_node = classes[0]
    if class_node.has_error:
        raise ValueError("The class declaration contains unresolved parse errors")
    members = {}
    body = class_node.child_by_field_name("body")
    access = "public" if class_node.type == "struct_specifier" else "private"
    for node in body.named_children:
        if node.type == "access_specifier":
            access = text_at(encoded[header], node)
            continue
        candidates = [node] if node.type != "preproc_if" else list(walk(node))
        for candidate in candidates:
            decl = function_declarator(candidate)
            if decl is None:
                continue
            key, qualified = function_key(encoded[header], candidate, decl)
            if key in members:
                raise ValueError("Ambiguous class member: " + key)
            members[key] = {"id": "fn-" + hashlib.sha256(key.encode()).hexdigest()[:16],
                            "key": key, "name": qualified.split("::")[-1], "qualified_name": qualified,
                            "signature": text_at(encoded[header], candidate).split("{")[0].strip().rstrip(";"),
                            "access": access, "declaration": reference(header, candidate),
                            "declaration_source": text_at(encoded[header], candidate), "implementation": None}
    for path, tree in trees.items():
        for node in walk(tree.root_node):
            if node.type != "function_definition":
                continue
            decl = function_declarator(node)
            if decl is None:
                continue
            key, qualified = function_key(encoded[path], node, decl)
            if not qualified.startswith(class_name + "::"):
                continue
            if key not in members:
                raise ValueError("Implementation does not match a declared member: " + key)
            member = members[key]
            if member["implementation"] is not None:
                raise ValueError("Multiple implementations require explicit build selection: " + key)
            if node.has_error:
                raise ValueError("Function has unresolved parse errors: " + key)
            member["implementation"] = reference(path, node)
            member["implementation_source"] = text_at(encoded[path], node)
            lines = len([line for line in member["implementation_source"].splitlines() if line.strip()])
            branches = sum(n.type in {"if_statement", "switch_statement", "for_statement", "while_statement"} for n in walk(node))
            if lines <= 8 and branches == 0:
                limits = {"prose_characters": 160, "steps": 2, "boundaries": 1, "semantics": 0}
            elif lines <= 16 and branches <= 2:
                limits = {"prose_characters": 320, "steps": 4, "boundaries": 2, "semantics": 1}
            else:
                limits = {"prose_characters": 600, "steps": 6, "boundaries": 4, "semantics": 2}
            member["explanation_limits"] = limits
    if not members:
        raise ValueError("No functions were resolved")
    missing = [key for key, member in members.items() if member["implementation"] is None]
    if missing:
        raise ValueError("Complete implementations are required for this pilot: " + ", ".join(missing))
    dependencies = []
    seen = set(paths)
    for path, source in sources.items():
        for include in re.findall(r'^\s*#include\s+"([^"]+)"', source, re.M):
            candidates = [str(PurePosixPath(path).parent / include), include]
            found = False
            for candidate in candidates:
                if candidate in seen:
                    found = True
                    break
                try:
                    text = source_text(repo, commit, candidate)
                except subprocess.CalledProcessError:
                    continue
                seen.add(candidate)
                found = True
                if len(text) > 16000:
                    dependencies.append({"file_path": candidate, "status": "omitted_size_limit"})
                else:
                    dependencies.append({"file_path": candidate, "status": "available", "source": text,
                                         "start_line": 1, "end_line": len(text.splitlines())})
                break
            if not found:
                dependencies.append({"include": include, "status": "unavailable_in_frozen_repository"})
    context = {"class_name": class_name, "scope": scope, "commit": commit,
               "parser_statement_macros": ["__TRY__", "__CATCH__"],
               "class_declaration": {**reference(header, class_node), "source": text_at(encoded[header], class_node)},
               "functions": list(members.values()), "dependencies": dependencies,
               "source_hashes": {p: hashlib.sha256(s.encode()).hexdigest() for p, s in sources.items()}}
    if len(str(context)) > 140000:
        raise ValueError("Class context exceeds the pilot budget; explicit partitioning is required")
    return context


def function_context(context, function):
    implementation = function["implementation_source"]
    peers = [f for f in context["functions"] if f["id"] != function["id"]
             and re.search(r"\b" + re.escape(f["name"]) + r"\b", implementation)]
    payload = {"class_name": context["class_name"], "commit": context["commit"],
               "parser_statement_macros": context["parser_statement_macros"],
               "class_declaration": context["class_declaration"], "target": function,
               "direct_class_dependencies": peers, "dependencies": context["dependencies"]}
    if len(str(payload)) > 80000:
        raise ValueError("Function context is too large; refusing to truncate its implementation")
    return payload
