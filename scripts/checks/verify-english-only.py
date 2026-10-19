# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 alibaba/open-code-review Contributors

"""Check source prose while allowing symbols and explicit localization exceptions."""

from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from tooling import ROOT, git_files, run_cli

EXTENSIONS = {
    ".go", ".kt", ".kts", ".ts", ".tsx", ".js", ".cjs", ".mjs", ".py", ".sh",
    ".ps1", ".css", ".html", ".yml", ".yaml", ".json",
}
ALLOWED_PREFIXES = {
    "internal/platform/ui/i18n/": "Localized workbench copy; identifiers and comments remain English.",
}
EXEMPT_MARKER = "allow-non-english:"


def prohibited_pattern():
    try:
        import regex
    except ImportError as error:
        raise RuntimeError("Install check dependencies: python -m pip install -r scripts/requirements.txt") from error
    # Script properties preserve the exemption for mathematical and icon letters.
    return regex.compile(
        r"[[\p{L}]--[\p{sc=Common}\p{sc=Inherited}\x00-\x7f]]"
        r"|[\u0300-\u036f\u3000-\u303f\ufe10-\ufe19\ufe30-\ufe6f\uff00-\uffef]",
        regex.VERSION1,
    )


def scan(text, pattern):
    for number, line in enumerate(text.splitlines(), 1):
        if EXEMPT_MARKER in line:
            continue
        match = pattern.search(line)
        if match:
            yield number, match.group(), line.strip()[:100]


def main():
    pattern = prohibited_pattern()
    checked = failed = 0
    for name in git_files():
        path = ROOT / name
        if path.suffix not in EXTENSIONS and path.name != "Makefile":
            continue
        if any(name.startswith(prefix) for prefix in ALLOWED_PREFIXES) or not path.is_file():
            continue
        checked += 1
        for line, char, text in scan(path.read_text(encoding="utf-8"), pattern):
            failed += 1
            print(f"{name}:{line}: {char!r} in {text}", file=sys.stderr)
    if failed:
        print(f"Found {failed} non-English lines. Use a reasoned {EXEMPT_MARKER} marker or a localized table.", file=sys.stderr)
        return 1
    print(f"No unapproved non-English text in {checked} scanned source files.")
    return 0


if __name__ == "__main__":
    sys.exit(run_cli(main))
