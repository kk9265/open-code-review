# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 alibaba/open-code-review Contributors

"""Verify or add source license headers without transcoding source files."""

import argparse
import codecs
from datetime import date
from pathlib import Path
import re
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from tooling import ROOT, git_files, run_cli

EXTENSIONS = {".go", ".js", ".mjs", ".ts", ".tsx", ".kt", ".kts", ".sh", ".py", ".css"}
IGNORED = {"vendor", "dist", "node_modules", "testdata"}
SPDX = b"SPDX-License-Identifier: Apache-2.0"
COPYRIGHT = re.compile(rb"Copyright ([0-9]{4}) alibaba/open-code-review Contributors")


def header_problem(raw):
    header = b"\n".join(raw.splitlines()[:20])
    if SPDX not in header:
        return "missing SPDX identifier"
    match = COPYRIGHT.search(header)
    if not match:
        return "missing copyright notice"
    if not 2026 <= int(match[1]) <= date.today().year:
        return "invalid copyright year"
    return None


def with_header(raw, suffix):
    if raw.startswith((codecs.BOM_UTF16_LE, codecs.BOM_UTF16_BE, codecs.BOM_UTF32_BE)):
        raise ValueError("UTF-16/32 sources require manual license editing.")
    bom = codecs.BOM_UTF8 if raw.startswith(codecs.BOM_UTF8) else b""
    body = raw[len(bom):]
    newline = b"\r\n" if b"\r\n" in body else b"\n"
    copyright_line = b"Copyright 2026 alibaba/open-code-review Contributors"
    if suffix == ".css":
        header = newline.join((b"/*", SPDX, copyright_line, b"*/"))
    else:
        prefix = b"# " if suffix in {".py", ".sh"} else b"// "
        header = newline.join((prefix + SPDX, prefix + copyright_line))
    lines = body.splitlines(keepends=True)
    leading = 1 if lines and lines[0].startswith(b"#!") else 0
    if suffix == ".py":
        for index, line in enumerate(lines[:2]):
            if re.match(rb"^[ \t\f]*#.*?coding[:=][ \t]*[-\w.]+", line):
                leading = index + 1
    start = b"".join(lines[:leading])
    if start and not start.endswith((b"\n", b"\r")):
        start += newline
    return bom + start + (newline if start else b"") + header + newline * 2 + b"".join(lines[leading:])


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("check", "add"), nargs="?", default="check")
    args = parser.parse_args(argv)
    failed = added = 0
    for name in git_files():
        path = ROOT / name
        if path.suffix not in EXTENSIONS or IGNORED.intersection(Path(name).parts):
            continue
        if not path.is_file():
            continue
        if path.is_symlink():
            raise RuntimeError(f"Refusing a source symlink: {name}")
        raw = path.read_bytes()
        problem = header_problem(raw)
        if problem is None:
            continue
        if args.action == "add" and problem != "invalid copyright year":
            path.write_bytes(with_header(raw, path.suffix))
            added += 1
            print(f"Added: {name}")
        else:
            failed += 1
            print(f"{name}: {problem}", file=sys.stderr)
    if failed:
        print("Use python scripts/licenses/license.py add to add missing headers.", file=sys.stderr)
        return 1
    print(f"Added headers to {added} files." if args.action == "add" else "All source files have valid license headers.")
    return 0


if __name__ == "__main__":
    sys.exit(run_cli(main))
