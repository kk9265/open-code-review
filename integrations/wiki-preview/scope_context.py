# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 alibaba/open-code-review Contributors

"""Extract frozen source and fulfill model-selected source requests without inference."""

import hashlib
from pathlib import PurePosixPath
import posixpath
import re
import subprocess

from cpp_context import parse_functions
from catalog_filter import excluded_path
from cpp_context import enclosing_names, reference, source_text, text_at, walk


class ScopeSources:
    def __init__(self, repo, commit, scope):
        from tree_sitter import Language, Parser
        import tree_sitter_cpp

        if scope != "LoginServer":
            raise ValueError("Only LoginServer expansion is authorized")
        self.repo, self.commit, self.scope = repo, commit, scope
        self.parser = Parser(Language(tree_sitter_cpp.language()))
        self.paths = set(subprocess.run(["git", "-C", str(repo), "ls-tree", "-r", "--name-only", commit],
                                       capture_output=True, check=True).stdout.decode("utf-8").splitlines())
        self.texts, self.parsed = {}, {}

    def read(self, path):
        if path not in self.paths:
            raise ValueError("Requested source is absent from the frozen Git tree: " + path)
        if path not in self.texts:
            self.texts[path] = source_text(self.repo, self.commit, path)
        return self.texts[path]

    def functions(self, path):
        if path not in self.parsed:
            self.parsed[path] = parse_functions(path, self.read(path), self.parser)
        return self.parsed[path]

    def file_unit(self, path):
        text = self.read(path)
        return {"id": "file-" + hashlib.sha256(path.encode()).hexdigest()[:16], "name": path, "source": text,
                "reference": {"file_path": path, "start_line": 1, "end_line": len(text.splitlines())}}

    def inventory(self):
        files = sorted(p for p in self.paths if p.startswith(self.scope + "/") and not excluded_path(p)
                       and not PurePosixPath(p).name.startswith("LoginWhiteList."))
        functions = []
        for path in files:
            self.read(path)
            if path.endswith((".cpp", ".h")):
                for value in self.functions(path):
                    lines = sum(bool(line.strip()) for line in value["source"].splitlines())
                    limits = ({"prose_characters": 160, "steps": 2, "boundaries": 1, "semantics": 0} if lines <= 8 else
                              {"prose_characters": 280, "steps": 3, "boundaries": 2, "semantics": 1} if lines <= 20 else
                              {"prose_characters": 480, "steps": 5, "boundaries": 3, "semantics": 1} if lines <= 150 else
                              {"prose_characters": 600, "steps": 6, "boundaries": 4, "semantics": 2})
                    functions.append({**value, "signature": value["key"], "explanation_limits": limits})
        return {"scope": self.scope, "commit": self.commit, "files": files, "functions": functions,
                "source_hashes": {p: hashlib.sha256(self.read(p).encode()).hexdigest() for p in files},
                "excluded_files": sorted(p for p in self.paths if p.startswith(self.scope + "/") and excluded_path(p))}

    def dependencies(self, paths):
        available, missing = set(), {}
        for path in sorted(paths):
            for include in re.findall(r'^\s*#include\s+"([^"]+)"', self.read(path), re.M):
                candidates = {include, posixpath.normpath(str(PurePosixPath(path).parent / include))}
                candidates.update(p for p in self.paths if p.endswith("/" + include))
                found = sorted(candidates & self.paths)
                available.update(found)
                definitions = []
                if not found:
                    base = PurePosixPath(include).name.rsplit(".", 1)[0]
                    definitions = sorted(p for p in self.paths if PurePosixPath(p).name == base + ".sdp")
                    available.update(definitions)
                    identity = "missing-" + hashlib.sha256(include.encode()).hexdigest()[:16]
                    missing[identity] = {"id": identity, "include": include, "status": "absent_from_frozen_tree",
                                         "available_definitions": definitions}
                for candidate in [*found, *definitions]:
                    stem = candidate.rsplit(".", 1)[0]
                    available.update(p for p in (stem + ".cpp", stem + "Imp.cpp", stem + ".h") if p in self.paths)
        return sorted(available), list(missing.values())

    def initial_context(self, functions, files):
        units = {f["id"]: f for f in functions}
        paths = set(files) | {f["reference"]["file_path"] for f in functions}
        declarations = {p.rsplit(".", 1)[0] + ".h" for p in paths if p.endswith(".cpp")}
        paths.update(declarations & self.paths)
        available, missing = self.dependencies(paths)
        boundaries = []
        for path in sorted(paths):
            # Full files with parser errors remain available for independent boundary checks.
            needs_full = not path.endswith(".cpp") or any(f.get("parse_error") and f["reference"]["file_path"] == path for f in functions)
            if needs_full:
                unit = self.file_unit(path)
                units[unit["id"]] = unit
        for path in available:
            if path.endswith((".h", ".sdp")) and len(self.read(path)) <= 12000:
                unit = self.file_unit(path)
                if sum(len(u["source"]) for u in units.values()) + len(unit["source"]) <= 110000:
                    units[unit["id"]] = unit
                else:
                    boundaries.append({"file_path": path, "status": "not_included_context_budget"})
        if sum(len(u["source"]) for u in units.values()) > 160000:
            raise ValueError("Initial module context exceeds budget; split the model plan instead of truncating source")
        return {"units": list(units.values()), "unavailable": missing, "available_files": available,
                "boundaries": boundaries, "parser_statement_macros": ["__TRY__", "__CATCH__"],
                "resolution": "Calls are syntax only. Full original bodies are supplied. parse_error units need their full file for boundary verification. Source requests resolve exact paths/symbols, never short-name call edges."}

    def fetch(self, requests, context):
        units = {u["id"]: u for u in context["units"]}
        unavailable = {u["id"]: u for u in context["unavailable"]}
        boundaries = list(context["boundaries"])
        for request in requests:
            path, symbol = request["file_path"], request["symbol"]
            parsed_path = PurePosixPath(path)
            if parsed_path.is_absolute() or ".." in parsed_path.parts or ":" in path or "\\" in path:
                raise ValueError("Source requests must use normalized repository-relative paths")
            if path not in self.paths:
                identity = "missing-" + hashlib.sha256(path.encode()).hexdigest()[:16]
                unavailable[identity] = {"id": identity, "include": path, "status": "absent_from_frozen_tree"}
                continue
            if parsed_path.suffix not in {".cpp", ".cc", ".c", ".h", ".hpp", ".inl", ".sdp", ".example", ".txt"}:
                raise ValueError("Unsupported source request extension")
            matches = [f for f in self.functions(path) if f["name"] == symbol or f["key"] == symbol] if symbol else []
            if symbol and not matches and "(" in symbol:
                qualified = symbol.split("(", 1)[0].strip()
                matches = [f for f in self.functions(path) if f["name"] == qualified]
                if matches:
                    boundaries.append({"file_path": path, "symbol": symbol,
                        "status": "requested_signature_not_matched_all_exact_name_overloads_supplied",
                        "available_keys": [f["key"] for f in matches]})
            if symbol and not matches and "(" not in symbol:
                data = self.read(path).encode("utf-8")
                normalized = re.sub(rb"(?m)^(\s*)(__(?:TRY|CATCH)__)(\s*)$",
                                    lambda match: match[1] + b" " * len(match[2]) + match[3], data)
                for node in walk(self.parser.parse(normalized).root_node):
                    if node.type not in {"class_specifier", "struct_specifier"}:
                        continue
                    name = node.child_by_field_name("name")
                    if name is not None and "::".join([*enclosing_names(node), text_at(data, name)]) == symbol:
                        matches.append({"id": "class-" + hashlib.sha256((path + ":" + symbol).encode()).hexdigest()[:16],
                                        "name": symbol, "source": text_at(data, node), "reference": reference(path, node),
                                        "parse_error": node.has_error})
            if symbol and not matches:
                boundaries.append({"file_path": path, "symbol": symbol, "status": "symbol_not_resolved_in_available_file"})
                continue
            additions = matches or [self.file_unit(path)]
            for unit in additions:
                if unit["id"] in units:
                    continue
                if sum(len(u["source"]) for u in units.values()) + len(unit["source"]) > 160000:
                    boundaries.append({"file_path": path, "symbol": symbol, "status": "not_included_context_budget"})
                else:
                    units[unit["id"]] = unit
        return {**context, "units": list(units.values()), "unavailable": list(unavailable.values()), "boundaries": boundaries}
