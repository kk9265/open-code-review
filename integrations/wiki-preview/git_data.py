# SPDX-License-Identifier: Apache-2.0
import hashlib
import json
import os
import subprocess
from pathlib import Path, PurePosixPath


def digest(value):
    return hashlib.sha256(
        json.dumps(value, sort_keys=True, ensure_ascii=True).encode()
    ).hexdigest()


def git(repo, *args, input_data=None):
    return subprocess.check_output(
        ["git", "-c", "protocol.allow=never", "-C", str(repo), *args],
        stderr=subprocess.PIPE,
        timeout=120,
        input=input_data,
        env={**os.environ, "GIT_NO_LAZY_FETCH": "1", "GIT_TERMINAL_PROMPT": "0"},
    )


def blob_hashes(repo, entries):
    return {
        path: value["sha256"] for path, value in blob_metadata(repo, entries).items()
    }


def blob_metadata(repo, entries):
    oids = sorted({entry["oid"] for entry in entries})
    body = git(
        repo,
        "cat-file",
        "--batch",
        input_data="".join(oid + "\n" for oid in oids).encode("ascii"),
    )
    result = {}
    offset = 0
    for oid in oids:
        end = body.index(b"\n", offset)
        actual, kind, size = body[offset:end].decode("ascii").split()
        size = int(size)
        offset = end + 1
        if actual != oid or kind != "blob":
            raise ValueError("Expected a Git blob: " + oid)
        content = body[offset : offset + size]
        text = (
            content.decode("utf-8", errors="replace")
            .replace("\r\n", "\n")
            .replace("\r", "\n")
        )
        result[oid] = {
            "sha256": hashlib.sha256(content).hexdigest(),
            "text_sha256": hashlib.sha256(text.encode("utf-8")).hexdigest(),
            "line_count": content.count(b"\n")
            + int(bool(content) and not content.endswith(b"\n")),
        }
        offset += size + 1
    return {entry["path"]: result[entry["oid"]] for entry in entries}


def repository(path):
    root = Path(git(path, "rev-parse", "--show-toplevel").decode().strip()).resolve()
    return root, digest(str(root).casefold() if os.name == "nt" else str(root))


def commit(repo, ref):
    return (
        git(repo, "rev-parse", "--verify", "--end-of-options", ref + "^{commit}")
        .decode()
        .strip()
    )


def scope_paths(paths):
    result = []
    for value in paths:
        value = value.replace("\\", "/").rstrip("/")
        path = PurePosixPath(value)
        if not value or path.is_absolute() or ".." in path.parts or ":" in value:
            raise ValueError("Scope must contain repository-relative paths")
        result.append(path.as_posix())
    return sorted(set(result))


def in_scope(path, scope):
    return not scope or any(
        path == item or path.startswith(item + "/") for item in scope
    )


def tree(repo, sha, scope):
    result = []
    for record in git(repo, "ls-tree", "-rz", "--full-tree", sha).split(b"\0"):
        if not record:
            continue
        header, name = record.split(b"\t", 1)
        mode, kind, oid = header.decode().split()
        path = name.decode("utf-8")
        if in_scope(path, scope):
            result.append({"path": path, "mode": mode, "kind": kind, "oid": oid})
    return result


def changes(repo, base, head, scope):
    if base is None:
        return [{"status": "A", "path": f["path"]} for f in tree(repo, head, scope)]
    parts = git(
        repo,
        "diff",
        "--no-ext-diff",
        "--no-textconv",
        "--name-status",
        "-z",
        "-M",
        base,
        head,
        "--",
    ).split(b"\0")
    output = []
    index = 0
    while index < len(parts) and parts[index]:
        status = parts[index].decode()
        path = parts[index + 1].decode("utf-8")
        index += 2
        item = {"status": status, "path": path}
        if status[0] in {"R", "C"}:
            item = {
                "status": status,
                "old_path": path,
                "path": parts[index].decode("utf-8"),
            }
            index += 1
        if in_scope(item["path"], scope) or in_scope(
            item.get("old_path", item["path"]), scope
        ):
            output.append(item)
    return output


def catalog(path, baseline):
    if path is None:
        return []
    data = json.loads(Path(path).read_text(encoding="utf-8"))
    version = data.get("commit")
    if version is None and len(data.get("repositories", [])) == 1:
        version = data["repositories"][0]["commit_hash"]
    if baseline is None or version != baseline:
        raise ValueError("Catalog commit must equal the baseline commit")
    result = []
    keys = set()
    for page in data["pages"]:
        language = page.get("language_code", "zh")
        key = language + "/" + page["slug"]
        if key in keys:
            raise ValueError("Duplicate catalog page: " + key)
        keys.add(key)
        refs = page.get("source_files")
        if refs is None:
            raw = page.get(
                "source_refs", json.loads(page.get("source_refs_json", "[]"))
            )
            refs = [ref["file_path"] for ref in raw]
        result.append(
            {
                "key": key,
                "title": page.get("title", page["slug"]),
                "parent": language + "/" + page["parent_slug"]
                if page.get("parent_slug")
                else None,
                "source_files": sorted(set(refs)),
                "global": bool(page.get("global", False)),
            }
        )
    return sorted(result, key=lambda item: item["key"])
