# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 alibaba/open-code-review Contributors

"""Explicitly discard generated Wiki outputs without touching source or graphs."""

import argparse
from pathlib import Path
import shutil
import stat

from publish import atomic_json

SCOPES = ("LoginServer", "AccountServer")
COMMIT = "9052b9e77e681315a200a8489dfc6938d81024db"


def reset_generated(data):
    data = Path(data).resolve(strict=True)
    roots = [data / ("wiki-" + scope.lower() + "-generated") for scope in SCOPES]
    for root in roots:
        if root.resolve().parent != data:
            raise ValueError("Generated output must be directly inside the data directory")
        if root.exists() and not root.is_dir():
            raise ValueError("Generated output must be a directory")
        if (root / "function-guide.lock").exists():
            raise ValueError("A class guide is running; stop it before discarding generated output")
        if root.exists():
            for path in [root, *root.rglob("*")]:
                if path.is_symlink() or getattr(path.lstat(), "st_file_attributes", 0) & stat.FILE_ATTRIBUTE_REPARSE_POINT:
                    raise ValueError("Refusing to reset an output containing links or junctions")
    for scope, root in zip(SCOPES, roots):
        root.mkdir(exist_ok=True)
        for path in root.iterdir():
            if path.name == "generation-model.json" or (path.is_file() and path.suffix == ".py"):
                continue
            if path.is_dir():
                shutil.rmtree(path)
            else:
                path.unlink()
        atomic_json(root / "manifest.json", {"repository": "zgame", "scope": scope, "commit": COMMIT,
                                             "assistant_authorship": False, "pages": []})
        atomic_json(root / "progress.json", {"planned_pages": 0, "completed_pages": 0,
                                             "model": "", "generated_models": {}, "failures": {}})
    return roots


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--data-dir", type=Path, default=Path(__file__).resolve().parents[2] / ".ocr-data")
    parser.add_argument("--discard-all-generated", action="store_true", required=True)
    args = parser.parse_args()
    for root in reset_generated(args.data_dir):
        print("Cleared generated Wiki:", root)


if __name__ == "__main__":
    main()
