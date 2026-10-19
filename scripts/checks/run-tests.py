# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 alibaba/open-code-review Contributors

"""Run local-only Python tests without placing them beside application code."""

import argparse
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
SUITES = {
    "tools": (ROOT / ".local-tests/python/tools", ROOT / "scripts"),
    "wiki": (ROOT / ".local-tests/python/wiki-preview", ROOT / "integrations/wiki-preview"),
}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--suite", choices=("all", *SUITES), default="all")
    args = parser.parse_args(argv)
    selected = list(SUITES) if args.suite == "all" else [args.suite]
    for name in selected:
        directory, _ = SUITES[name]
        if not directory.is_dir() or not any(directory.glob("test_*.py")):
            parser.error(f"No local tests for {name}. Restore .local-tests from your local backup first.")
    for name in selected:
        directory, source = SUITES[name]
        environment = os.environ.copy()
        import_paths = [str(source), str(directory), environment.get("PYTHONPATH", "")]
        environment["PYTHONPATH"] = os.pathsep.join(path for path in import_paths if path)
        environment["LC_ALL"] = "C"
        result = subprocess.run(
            [sys.executable, "-m", "unittest", "discover", "-s", str(directory), "-p", "test_*.py", "-v"],
            cwd=ROOT, env=environment,
        )
        if result.returncode:
            return result.returncode
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except KeyboardInterrupt:
        sys.exit(130)
