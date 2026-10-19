# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 alibaba/open-code-review Contributors

"""Start the personal workbench, optionally configuring its Gitea token."""

import argparse
import getpass
import os
from pathlib import Path
import subprocess
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from tooling import ROOT, WINDOWS, process_module, run_cli, save_token, saved_token, workbench_processes


def child_environment(data_dir, gitea=False, update_token=False):
    environment = os.environ.copy()
    present = environment.get("OCR_GITEA_TOKEN", "").strip()
    token_file = data_dir / "gitea-token.dpapi"
    if update_token or (gitea and not present and not token_file.is_file()):
        token = getpass.getpass("Paste the Gitea token: ").strip()
        if not token:
            raise ValueError("A Gitea token is required.")
        if WINDOWS:
            save_token(data_dir, token)
            print("Token saved encrypted for the current Windows user.")
        environment["OCR_GITEA_TOKEN"] = token
    elif not present and token_file.is_file():
        environment["OCR_GITEA_TOKEN"] = saved_token(data_dir)
    return environment


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--gitea", action="store_true")
    parser.add_argument("--update-gitea-token", action="store_true")
    parser.add_argument("--restart", action="store_true")
    parser.add_argument("--addr", default="127.0.0.1:5484")
    parser.add_argument("--data-dir", type=Path, default=ROOT / ".ocr-data")
    args = parser.parse_args(argv)
    executable = (ROOT / "dist" / ("ocr-web.exe" if WINDOWS else "ocr-web")).resolve()
    if not executable.is_file():
        raise RuntimeError(f"Build first: python scripts/toolchain/go.py build -o {executable} ./cmd/ocr-web")
    running = [process for process, path in workbench_processes() if path == executable]
    if running and not args.restart:
        raise RuntimeError("This workbench is running. Use --restart to replace its process.")
    environment = child_environment(args.data_dir.resolve(), args.gitea, args.update_gitea_token)
    psutil = process_module()
    for process in running:
        try:
            process.terminate()
            process.wait(timeout=5)
        except psutil.NoSuchProcess:
            pass
        except psutil.TimeoutExpired as error:
            raise RuntimeError("Workbench did not stop; no new instance was started.") from error
    print(f"Opening workbench at http://{args.addr} (Ctrl+C to stop).", flush=True)
    return subprocess.call(
        [str(executable), "-addr", args.addr, "-data-dir", str(args.data_dir.resolve())],
        cwd=ROOT, env=environment,
    )


if __name__ == "__main__":
    sys.exit(run_cli(main))
