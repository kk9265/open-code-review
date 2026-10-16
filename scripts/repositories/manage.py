# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 alibaba/open-code-review Contributors

"""Maintain workbench repository caches without the Web API."""

import argparse
import ctypes
import json
import os
from pathlib import Path
import shutil
import signal
import sqlite3
import subprocess
import sys
from contextlib import contextmanager


ROOT = Path(__file__).resolve().parents[2]
WINDOWS = sys.platform == "win32"


def build_tool():
    suffix = ".exe" if WINDOWS else ""
    tool = ROOT / "dist" / ("ocr-repo" + suffix)
    sdk = ROOT / ".local-go" / "sdk" / "go" / "bin" / ("go" + suffix)
    go = str(sdk) if sdk.is_file() else shutil.which("go")
    if not go:
        raise RuntimeError("Go is unavailable. Prepare the project Go 1.26.9 toolchain first.")
    env = os.environ.copy()
    env.pop("GOROOT", None)
    env.update(
        GOENV="off", GOTOOLCHAIN="go1.26.9",
        GOPATH=str(ROOT / ".local-go" / "work"),
        GOMODCACHE=str(ROOT / ".local-go" / "work" / "pkg" / "mod"),
        GOCACHE=str(ROOT / ".local-go" / "build-cache"),
        GOOS="windows" if WINDOWS else "linux",
    )
    # Let Go select the host architecture instead of inheriting cross-build settings.
    env.pop("GOARCH", None)
    subprocess.run([go, "build", "-o", str(tool), "./cmd/ocr-repo"], cwd=ROOT, env=env, check=True)
    return tool


def saved_token(data_dir):
    token = os.environ.get("OCR_GITEA_TOKEN", "").strip()
    if token:
        return token
    if not WINDOWS:
        raise RuntimeError("Set OCR_GITEA_TOKEN in the process environment before fetching.")
    token_file = data_dir / "gitea-token.dpapi"
    if not token_file.is_file():
        raise RuntimeError("No saved token. Configure it with scripts/workbench/start-web.ps1 -Gitea first.")
    from ctypes import wintypes

    class Blob(ctypes.Structure):
        _fields_ = [("size", wintypes.DWORD), ("data", ctypes.POINTER(ctypes.c_ubyte))]

    crypt = ctypes.WinDLL("crypt32", use_last_error=True)
    kernel = ctypes.WinDLL("kernel32", use_last_error=True)
    crypt.CryptUnprotectData.argtypes = [ctypes.POINTER(Blob), ctypes.c_void_p, ctypes.c_void_p,
                                        ctypes.c_void_p, ctypes.c_void_p, wintypes.DWORD, ctypes.POINTER(Blob)]
    crypt.CryptUnprotectData.restype = wintypes.BOOL
    kernel.LocalFree.argtypes = [ctypes.c_void_p]
    kernel.LocalFree.restype = ctypes.c_void_p
    raw = bytes.fromhex(token_file.read_text(encoding="utf-8-sig").strip())
    buffer = (ctypes.c_ubyte * len(raw)).from_buffer_copy(raw)
    source = Blob(len(raw), buffer)
    result = Blob()
    if not crypt.CryptUnprotectData(ctypes.byref(source), None, None, None, None, 1, ctypes.byref(result)):
        raise RuntimeError("Cannot decrypt the saved token. Run as the Windows user who saved it.")
    try:
        return ctypes.string_at(result.data, result.size).decode("utf-16-le").strip()
    finally:
        ctypes.memset(result.data, 0, result.size)
        kernel.LocalFree(result.data)


def records_for(tool, data_dir):
    result = subprocess.run([str(tool), "-data-dir", str(data_dir), "-action", "list"],
                            check=True, capture_output=True, text=True, encoding="utf-8")
    return json.loads(result.stdout)


def directory_argument(arguments, cwd, environment):
    value = environment.get("OCR_WEB_DATA_DIR", ".ocr-data")
    for index, argument in enumerate(arguments[1:], 1):
        if argument.startswith("-data-dir="):
            value = argument.split("=", 1)[1]
        elif argument == "-data-dir":
            if index + 1 >= len(arguments):
                raise RuntimeError("Cannot determine the workbench data directory.")
            value = arguments[index + 1]
    path = Path(value)
    return (path if path.is_absolute() else cwd / path).resolve()


@contextmanager
def paused_workbench(enabled, tool, data_dir):
    if not enabled:
        yield
        return
    try:
        import psutil
    except ImportError as error:
        raise RuntimeError("Install pause/resume support: python -m pip install -r scripts/repositories/requirements.txt") from error
    executable = (ROOT / "dist" / ("ocr-web.exe" if WINDOWS else "ocr-web")).resolve()
    candidates = []
    for process in psutil.process_iter(["name"]):
        if process.info["name"] != executable.name:
            continue
        try:
            if Path(process.exe()).resolve() != executable:
                continue
            arguments, cwd, environment = process.cmdline(), Path(process.cwd()), process.environ()
            if directory_argument(arguments, cwd, environment) == data_dir:
                candidates.append((process, arguments, cwd, environment))
        except psutil.NoSuchProcess:
            continue
        except psutil.AccessDenied as error:
            raise RuntimeError("Cannot inspect the workbench. Stop it manually or run as its owner.") from error
    if len(candidates) > 1:
        raise RuntimeError("Multiple workbench instances match. Stop them manually before maintenance.")
    if candidates and any(record["State"] in ("running", "queued") for record in records_for(tool, data_dir)):
        raise RuntimeError("Repository sync is active or queued. Wait for it to finish before pausing.")
    if candidates:
        database = sqlite3.connect((data_dir / "workbench.db").as_uri() + "?mode=ro", uri=True)
        try:
            if database.execute("SELECT 1 FROM sqlite_master WHERE name='review_queue'").fetchone():
                active = database.execute("SELECT enabled OR EXISTS(SELECT 1 FROM review_jobs WHERE state='running') FROM review_queue WHERE id=1").fetchone()
                if active and active[0]:
                    raise RuntimeError("Pause the AI review queue and wait for its current run before pausing the workbench.")
        finally:
            database.close()
    stopped = []
    try:
        for process, arguments, cwd, environment in candidates:
            process.terminate()
            try:
                process.wait(timeout=10)
            except psutil.TimeoutExpired as error:
                raise RuntimeError("Workbench did not stop. Maintenance was not started.") from error
            stopped.append((arguments, cwd, environment))
        yield
    finally:
        for arguments, cwd, environment in stopped:
            options = {"creationflags": subprocess.CREATE_NO_WINDOW} if WINDOWS else {"start_new_session": True}
            with (data_dir / "maintenance-web-stdout.log").open("ab") as stdout, (data_dir / "maintenance-web-stderr.log").open("ab") as stderr:
                subprocess.Popen(arguments, cwd=cwd, env=environment, stdin=subprocess.DEVNULL,
                                 stdout=stdout, stderr=stderr, **options)
            print("Workbench restarted with its original arguments and environment.", flush=True)


def maintain(command, environment):
    process = subprocess.Popen(command, env=environment)
    try:
        return process.wait()
    except KeyboardInterrupt:
        # Wait for Go to stop Git and release the lock before restarting the workbench.
        previous = signal.signal(signal.SIGINT, signal.SIG_IGN)
        try:
            if not WINDOWS and process.poll() is None:
                process.send_signal(signal.SIGINT)
            process.wait()
        finally:
            signal.signal(signal.SIGINT, previous)
        return 130


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("list", "fetch", "branches"), nargs="?", default="list")
    parser.add_argument("repository", nargs="?", help="repository ID, exact name, or owner/name")
    parser.add_argument("--data-dir", type=Path, default=ROOT / ".ocr-data")
    parser.add_argument("--local-source", type=Path, help="fetch from this local bare repository instead of Gitea HTTP")
    parser.add_argument("--pause-workbench", action="store_true", help="pause and restore the matching idle workbench")
    args = parser.parse_args()
    if sys.platform not in ("win32", "linux"):
        parser.error("Only Windows and Linux are supported.")
    if args.action != "list" and not args.repository:
        parser.error("Specify a repository ID, exact name, or owner/name.")
    if args.local_source and args.action != "fetch":
        parser.error("--local-source is only supported for fetch.")
    data_dir = args.data_dir.resolve()
    if not (data_dir / "workbench.db").is_file():
        parser.error("Existing workbench.db not found. Check --data-dir.")
    tool = build_tool()
    records = records_for(tool, data_dir)
    if args.action == "list":
        for record in records:
            print(f"{record['ID']:>4}  {record['FullName']}  [{record['State']}]\n      {record['CachePath']}")
        return 0
    matches = [record for record in records if args.repository in (str(record["ID"]), record["Name"], record["FullName"])]
    if len(matches) != 1:
        parser.error("Repository is missing or ambiguous. Use its ID from list.")
    if args.action == "fetch" and matches[0]["Archived"]:
        parser.error("Restore the archived repository before fetching.")
    environment = os.environ.copy()
    if args.action == "fetch" and not args.local_source and matches[0].get("SyncMode") != "local":
        environment["OCR_GITEA_TOKEN"] = saved_token(data_dir)
    with paused_workbench(args.pause_workbench, tool, data_dir):
        print("Direct cache maintenance. Fetch has no total timeout; Ctrl+C cancels.", flush=True)
        command = [str(tool), "-data-dir", str(data_dir), "-action", args.action,
                   "-repository", str(matches[0]["ID"])]
        if args.local_source:
            command.extend(["-local-source", str(args.local_source.resolve())])
        return maintain(command, environment)


if __name__ == "__main__":
    try:
        sys.exit(main())
    except KeyboardInterrupt:
        sys.exit(130)
    except (OSError, RuntimeError, ValueError, subprocess.CalledProcessError) as error:
        print(f"Maintenance failed: {error}", file=sys.stderr)
        sys.exit(1)
