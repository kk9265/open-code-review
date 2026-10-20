# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 alibaba/open-code-review Contributors

"""Partition reviewed function sections without generating explanatory text."""

import argparse
from collections import defaultdict
import hashlib
import json
from pathlib import Path, PurePosixPath
import re
from urllib.parse import parse_qs, unquote, urlsplit

from publish import atomic_json, publish


def partition_functions(pages, resolve_owner):
    result, groups, seen = [], defaultdict(list), set()
    for page in pages:
        if page.get("page_type") != "function-index":
            result.append(page)
            continue
        text = page["markdown"]
        headings = list(re.finditer(r"(?m)^## `[^\n]+` \{#([a-zA-Z0-9_-]+)\}\n", text))
        entries = {entry["id"]: entry for entry in page["function_index"]}
        if len(entries) != len(page["function_index"]) or {h[1] for h in headings} != set(entries):
            raise ValueError("Function sections do not match the reviewed inventory")
        for number, heading in enumerate(headings):
            identity = heading[1]
            if identity in seen:
                raise ValueError("Duplicate function identity")
            seen.add(identity)
            entry = entries[identity]
            end = headings[number + 1].start() if number + 1 < len(headings) else len(text)
            section = text[heading.start():end].strip() + "\n"
            references = []
            for target in re.findall(r"\]\((/wiki/read/source/[^\s)]+)\)", section):
                url = urlsplit(target)
                lines = re.fullmatch(r"L(\d+)-L(\d+)", url.fragment)
                if not lines or parse_qs(url.query).get("function") != [identity]:
                    raise ValueError("Function source link has no exact range or identity")
                reference = {"file_path": unquote(url.path.removeprefix("/wiki/read/source/")),
                             "start_line": int(lines[1]), "end_line": int(lines[2])}
                if reference not in page["source_refs"]:
                    raise ValueError("Function source link is not in the reviewed source references")
                references.append(reference)
            if not references:
                raise ValueError("Function has no source references")
            owner = resolve_owner(entry, references)
            group_slug = "functions-" + hashlib.sha256(owner.encode()).hexdigest()[:20]
            slug = "function-" + identity
            section = section.replace("page=" + page["slug"] + "&function=" + identity,
                                      "page=" + slug + "&function=" + identity)
            function = {"slug": slug, "title": entry["signature"], "parent_slug": group_slug,
                        "page_type": "function", "markdown": section.removeprefix("#"),
                        "generation_model": page["generation_model"], "function_index": [entry],
                        "section_index": [], "source_refs": references}
            groups[(owner, group_slug, page["generation_model"])].append(function)
    for (owner, slug, model), functions in sorted(groups.items()):
        functions.sort(key=lambda p: (p["title"], p["slug"]))
        title = PurePosixPath(owner).name if "/" in owner else owner
        body = "# `" + title.replace("`", "") + "`\n\n"
        body += "\n".join("- [`" + p["title"].replace("`", "") + "`](/wiki/read/page/" + p["slug"] + ") — "
                          + p["function_index"][0]["summary"] for p in functions) + "\n"
        result.append({"slug": slug, "title": title, "parent_slug": None, "page_type": "function-group",
                       "markdown": body, "generation_model": model, "function_index": [],
                       "section_index": [], "source_refs": []})
        result.extend(functions)
    if groups and sum(p.get("page_type") == "function" for p in result) != len(seen):
        raise ValueError("Function partition lost an explanation")
    return result


def source_owner(repo, commit):
    from cpp_context import parse_functions
    from cpp_context import source_text
    from tree_sitter import Language, Parser
    import tree_sitter_cpp

    parser = Parser(Language(tree_sitter_cpp.language()))
    cache = {}

    def resolve(entry, references):
        for reference in sorted(references, key=lambda r: not r["file_path"].endswith(".cpp")):
            path = reference["file_path"]
            if path not in cache:
                cache[path] = parse_functions(path, source_text(repo, commit, path), parser)
            matches = [f for f in cache[path] if f["reference"] == reference]
            if len(matches) == 1:
                name = matches[0]["name"]
                if name != entry["name"] and name.rsplit("::", 1)[-1] != entry["name"]:
                    raise ValueError("Function name differs from the frozen source")
                return name.rsplit("::", 1)[0] if "::" in name else path
        raise ValueError("Function owner is not resolved from its exact frozen source range")

    return resolve


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--data-dir", type=Path, default=Path(".ocr-data"))
    parser.add_argument("--scope", choices=["LoginServer"], required=True)
    parser.add_argument("--publish", action="store_true")
    args = parser.parse_args(argv)
    root = args.data_dir / "wiki-loginserver-generated"
    path = root / "manifest.json"
    baseline = path.read_bytes()
    manifest = json.loads(baseline)
    if manifest.get("scope") != args.scope or manifest.get("source") != "aimana":
        raise ValueError("Only reviewed AI Mana output in the requested scope can be partitioned")
    if not any(p.get("page_type") == "function-index" for p in manifest["pages"]):
        print(json.dumps({"status": "already_partitioned"}))
        return
    lock = root / "function-guide.lock"
    with lock.open("x", encoding="utf-8") as stream:
        stream.write("structural-function-partition\n")
    try:
        pages = partition_functions(manifest["pages"], source_owner(args.data_dir / "wiki-zgame-plan/zgame", manifest["commit"]))
        report = {"functions": sum(p.get("page_type") == "function" for p in pages),
                  "groups": sum(p.get("page_type") == "function-group" for p in pages),
                  "pages": len(pages), "model_calls": 0, "explanatory_text_changed": False}
        if args.publish:
            if path.read_bytes() != baseline:
                raise ValueError("Published content changed while partitioning")
            progress = json.loads((root / "progress.json").read_text(encoding="utf-8"))
            release = publish(root, {**manifest, "pages": pages, "function_layout": "owner-and-function"},
                              {**progress, "planned_pages": len(pages), "failures": {}})
            report["release"] = release
            atomic_json(root / "function-layout.json", report)
        print(json.dumps(report))
    finally:
        lock.unlink()


if __name__ == "__main__":
    main()
