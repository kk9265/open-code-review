# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 alibaba/open-code-review Contributors

"""Publish a completed Wiki generation run without rewriting page contents."""

import hashlib
import json
import os
from pathlib import Path
import re
import tempfile
import time



def atomic_json(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, temporary = tempfile.mkstemp(prefix=path.name + ".", suffix=".tmp", dir=path.parent)
    try:
        with os.fdopen(fd, "w", encoding="utf-8", newline="\n") as stream:
            json.dump(value, stream, ensure_ascii=True)
            stream.flush()
            os.fsync(stream.fileno())
        for attempt in range(7):
            try:
                os.replace(temporary, path)
                break
            except PermissionError:
                if attempt == 6:
                    raise
                time.sleep(min(0.02 * (2 ** attempt), 0.2))
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def publish(root, manifest, progress):
    pages = manifest["pages"]
    if not pages or len(pages) != progress["planned_pages"] or progress.get("failures"):
        raise ValueError("All planned pages must pass before publishing")
    seen = set()
    checked = []
    for page in pages:
        slug = page["slug"]
        if not re.fullmatch(r"[a-zA-Z0-9_-]+", slug) or slug in seen:
            raise ValueError("Invalid or duplicate page slug")
        seen.add(slug)
        checked.append({**page, "markdown_sha256": hashlib.sha256(page["markdown"].encode("utf-8")).hexdigest()})
    identity = hashlib.sha256(json.dumps(checked, sort_keys=True, ensure_ascii=True).encode()).hexdigest()
    root = Path(root)
    directory = root / "releases" / identity / "markdown"
    directory.mkdir(parents=True, exist_ok=True)
    for page in checked:
        path = directory / (page["slug"] + ".md")
        data = page["markdown"].encode("utf-8")
        if path.exists():
            if path.read_bytes() != data:
                raise ValueError("Immutable release content has changed")
        else:
            path.write_bytes(data)
    published = {**manifest, "pages": checked,
                 "markdown_directory": directory.relative_to(root).as_posix()}
    previous = root / "manifest.json"
    if previous.exists():
        body = previous.read_bytes()
        history = root / "history" / (hashlib.sha256(body).hexdigest() + ".json")
        history.parent.mkdir(exist_ok=True)
        if not history.exists():
            history.write_bytes(body)
    atomic_json(root / "progress.json", {**progress, "completed_pages": len(checked)})
    atomic_json(previous, published)
    return identity
