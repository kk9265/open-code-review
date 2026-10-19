# SPDX-License-Identifier: Apache-2.0
import argparse
import hashlib
import json
import subprocess
import sys
from importlib.metadata import version
from pathlib import Path

from worker import HERE, verify_lock


def verify_checkout(source, lock):
    head = (
        subprocess.check_output(["git", "-C", str(source), "rev-parse", "HEAD"])
        .decode()
        .strip()
    )
    if head != lock["commit"]:
        raise ValueError("Unexpected CodeWiki commit")
    for name, expected in lock["patched_files"].items():
        body = (source / name).read_bytes().replace(b"\r\n", b"\n")
        if hashlib.sha256(body).hexdigest() != expected:
            raise ValueError("Unexpected patched source: " + name)
    changed = (
        subprocess.check_output(
            ["git", "-C", str(source), "diff", "HEAD", "--name-only"]
        )
        .decode()
        .splitlines()
    )
    if set(changed) - set(lock["patched_files"]):
        raise ValueError("Unpinned tracked source modifications")


def main():
    parser = argparse.ArgumentParser(
        description="Prepare pinned CodeWiki from a local clone; never installs or generates"
    )
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--destination", type=Path)
    parser.add_argument("--verify-runtime", action="store_true")
    args = parser.parse_args()
    lock = verify_lock()
    source = args.source.resolve()
    if args.destination:
        source = args.destination.resolve()
        if source.exists():
            raise ValueError("Destination must not exist")
        subprocess.run(
            [
                "git",
                "clone",
                "--no-hardlinks",
                "--no-checkout",
                "--",
                str(args.source.resolve()),
                str(source),
            ],
            check=True,
        )
        subprocess.run(
            ["git", "-C", str(source), "checkout", "--detach", lock["commit"]],
            check=True,
        )
        subprocess.run(
            ["git", "-C", str(source), "apply", str(HERE / lock["patch"])], check=True
        )
    verify_checkout(source, lock)
    if args.verify_runtime:
        if sys.platform != "win32" or sys.version_info[:3] != (3, 12, 12):
            raise ValueError(
                "The graph runtime lock was validated on Windows CPython 3.12.12"
            )
        for line in (HERE / "requirements-windows-py312.lock").read_text().splitlines():
            if not line or line.startswith("#"):
                continue
            package, expected = line.split("==")
            if version(package) != expected:
                raise ValueError("Dependency version mismatch: " + package)
    print(
        json.dumps(
            {
                "verified": True,
                "source": str(source),
                "commit": lock["commit"],
                "runtime_checked": args.verify_runtime,
            }
        )
    )


if __name__ == "__main__":
    main()
