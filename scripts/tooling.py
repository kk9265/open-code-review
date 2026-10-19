# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 alibaba/open-code-review Contributors

"""Shared support for the repository's Python command-line tools."""

import ctypes
import os
from pathlib import Path
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parent.parent
WINDOWS = sys.platform == "win32"


def go_command(root=ROOT):
    local = root / ".local-go/sdk/go/bin" / ("go.exe" if WINDOWS else "go")
    executable = str(local) if local.is_file() else shutil.which("go")
    if not executable:
        raise RuntimeError("Go is unavailable. Install Go or prepare .local-go/sdk/go first.")
    environment = os.environ.copy()
    environment.pop("GOROOT", None)
    environment.update(
        GOENV="off", GOTOOLCHAIN="go1.26.9",
        GOPATH=str(root / ".local-go/work"),
        GOMODCACHE=str(root / ".local-go/work/pkg/mod"),
        GOCACHE=str(root / ".local-go/build-cache"),
    )
    return executable, environment


def git_files(root=ROOT, *, untracked=True, paths=()):
    command = ["git", "-C", str(root), "ls-files", "-z", "--cached"]
    if untracked:
        command += ["--others", "--exclude-standard"]
    result = subprocess.run(command + ["--", *paths], check=True, capture_output=True)
    return sorted({os.fsdecode(path) for path in result.stdout.split(b"\0") if path})


def process_module():
    try:
        import psutil
    except ImportError as error:
        raise RuntimeError("Install tool dependencies: python -m pip install -r scripts/requirements.txt") from error
    return psutil


def workbench_processes():
    psutil = process_module()
    found = []
    for process in psutil.process_iter(["name"]):
        try:
            if not (process.info["name"] or "").lower().startswith("ocr-"):
                continue
            executable = process.exe()
            if not executable:
                raise RuntimeError(f"Cannot inspect workbench process {process.pid}.")
            found.append((process, Path(executable).resolve()))
        except psutil.NoSuchProcess:
            continue
        except psutil.AccessDenied as error:
            raise RuntimeError(f"Cannot inspect workbench process {process.pid}; stop it manually first.") from error
    return found


def token_cipher(raw, *, encrypt=False):
    if not WINDOWS:
        raise RuntimeError("Saved DPAPI tokens require Windows. Set OCR_GITEA_TOKEN instead.")
    from ctypes import wintypes

    class Blob(ctypes.Structure):
        _fields_ = [("size", wintypes.DWORD), ("data", ctypes.POINTER(ctypes.c_ubyte))]

    crypt = ctypes.WinDLL("crypt32", use_last_error=True)
    kernel = ctypes.WinDLL("kernel32", use_last_error=True)
    function = crypt.CryptProtectData if encrypt else crypt.CryptUnprotectData
    function.argtypes = [ctypes.POINTER(Blob), ctypes.c_void_p, ctypes.c_void_p,
                         ctypes.c_void_p, ctypes.c_void_p, wintypes.DWORD, ctypes.POINTER(Blob)]
    function.restype = wintypes.BOOL
    kernel.LocalFree.argtypes = [ctypes.c_void_p]
    kernel.LocalFree.restype = ctypes.c_void_p
    buffer = (ctypes.c_ubyte * len(raw)).from_buffer_copy(raw)
    source, result = Blob(len(raw), buffer), Blob()
    try:
        if not function(ctypes.byref(source), None, None, None, None, 1, ctypes.byref(result)):
            raise RuntimeError("Cannot process the saved token. Use the Windows account that saved it.")
        return ctypes.string_at(result.data, result.size)
    finally:
        ctypes.memset(buffer, 0, len(raw))
        if result.data:
            ctypes.memset(result.data, 0, result.size)
            kernel.LocalFree(result.data)


def saved_token(data_dir):
    token = os.environ.get("OCR_GITEA_TOKEN", "").strip()
    if token:
        return token
    path = data_dir / "gitea-token.dpapi"
    if not path.is_file():
        raise RuntimeError("No saved token. Run python scripts/workbench/start-web.py --gitea first.")
    return token_cipher(bytes.fromhex(path.read_text(encoding="utf-8-sig").strip())).decode("utf-16-le").strip()


def save_token(data_dir, token):
    encoded = token_cipher(token.encode("utf-16-le"), encrypt=True).hex()
    data_dir.mkdir(parents=True, exist_ok=True)
    (data_dir / "gitea-token.dpapi").write_text(encoded, encoding="utf-8")


def run_cli(main):
    try:
        return main()
    except KeyboardInterrupt:
        return 130
    except (OSError, RuntimeError, ValueError, subprocess.CalledProcessError) as error:
        print(f"Error: {error}", file=sys.stderr)
        return 1
