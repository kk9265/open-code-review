# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 alibaba/open-code-review Contributors

"""Run the pinned project Go toolchain without changing the parent environment."""

from pathlib import Path
import subprocess
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from tooling import ROOT, go_command, run_cli


def main():
    executable, environment = go_command()
    return subprocess.call([executable, *sys.argv[1:]], cwd=ROOT, env=environment)


if __name__ == "__main__":
    sys.exit(run_cli(main))
