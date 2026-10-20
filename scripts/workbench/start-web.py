# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 alibaba/open-code-review Contributors

"""Start the personal workbench, optionally configuring its Gitea token."""

import argparse
import getpass
import json
import os
from pathlib import Path
import sqlite3
import subprocess
import sys
import time
from urllib import error as urlerror
from urllib import parse as urlparse
from urllib import request as urlrequest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from tooling import ROOT, WINDOWS, process_module, run_cli, save_token, saved_token, workbench_processes


def configured_context_endpoint(data_dir):
    database = data_dir / "workbench.db"
    if not database.is_file():
        return ""
    connection = sqlite3.connect(f"file:{database.as_posix()}?mode=ro", uri=True, timeout=1)
    try:
        row = connection.execute("SELECT payload FROM settings WHERE id=1").fetchone()
    finally:
        connection.close()
    if not row:
        return ""
    return str(json.loads(row[0]).get("context_endpoint", "")).strip().rstrip("/")


def context_service_ready(endpoint, timeout=0.5):
    body = json.dumps({"session_id": ""}).encode("utf-8")
    request = urlrequest.Request(endpoint + "/v1/close", data=body, headers={"Content-Type": "application/json"})
    try:
        with urlrequest.urlopen(request, timeout=timeout) as response:
            payload = json.loads(response.read().decode("utf-8"))
            return response.status == 200 and payload.get("closed") is True
    except (OSError, ValueError, urlerror.URLError):
        return False


def start_context_service(data_dir, environment, config_path):
    endpoint = configured_context_endpoint(data_dir)
    if not endpoint:
        return None
    if context_service_ready(endpoint):
        print(f"Reusing context service at {endpoint}.", flush=True)
        return None
    parsed = urlparse.urlparse(endpoint)
    if parsed.scheme != "http" or parsed.hostname != "127.0.0.1" or parsed.path not in ("", "/"):
        raise RuntimeError(f"Cannot auto-start unsupported context endpoint {endpoint}; use http://127.0.0.1:<port>.")
    try:
        port = parsed.port or 80
    except ValueError as error:
        raise RuntimeError(f"Invalid context endpoint {endpoint}.") from error
    if not config_path.is_file():
        raise RuntimeError(f"External context is enabled, but its config is missing: {config_path}")
    manager = ROOT / "scripts" / "context" / "manage.py"
    log_path = config_path.parent / "service.log"
    log_path.parent.mkdir(parents=True, exist_ok=True)
    with log_path.open("ab") as log:
        process = subprocess.Popen(
            [sys.executable, str(manager), "--config", str(config_path), "serve", "--port", str(port)],
            cwd=ROOT, env=environment, stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT,
        )
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        if context_service_ready(endpoint):
            print(f"Started context service at {endpoint}.", flush=True)
            return process
        if process.poll() is not None:
            raise RuntimeError(f"Context service exited during startup; inspect {log_path}.")
        time.sleep(0.1)
    process.terminate()
    process.wait(timeout=5)
    raise RuntimeError(f"Context service did not become ready; inspect {log_path}.")


def stop_context_service(process):
    if process is None or process.poll() is not None:
        return
    process.terminate()
    try:
        process.wait(timeout=5)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait(timeout=5)


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
    parser.add_argument("--context-config", type=Path, default=ROOT / ".ocr-context" / "config.json")
    parser.add_argument("--no-context", action="store_true", help="do not auto-start the configured context service")
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
    context_process = None
    try:
        if not args.no_context:
            context_process = start_context_service(
                args.data_dir.resolve(), environment, args.context_config.resolve(),
            )
        print(f"Opening workbench at http://{args.addr} (Ctrl+C to stop).", flush=True)
        return subprocess.call(
            [str(executable), "-addr", args.addr, "-data-dir", str(args.data_dir.resolve())],
            cwd=ROOT, env=environment,
        )
    finally:
        stop_context_service(context_process)


if __name__ == "__main__":
    sys.exit(run_cli(main))
