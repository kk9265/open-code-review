# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 alibaba/open-code-review Contributors

"""Preview or remove obsolete build files while retaining usable versions."""

import argparse
from dataclasses import dataclass
import os
from pathlib import Path
import re
import stat
import subprocess
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from tooling import ROOT, git_files, run_cli, workbench_processes

VARIANT = re.compile(r"ocr-(web|repo)-[a-zA-Z0-9][a-zA-Z0-9._-]*\.exe\Z")


@dataclass(frozen=True)
class Build:
    path: Path
    size: int
    modified_ns: int
    identity: tuple


def plain_path(path):
    for part in (path, *path.parents):
        info = part.lstat()
        if stat.S_ISLNK(info.st_mode) or getattr(info, "st_file_attributes", 0) & 0x400:
            raise RuntimeError(f"Refusing a path containing a link or junction: {part}")


def snapshot(path):
    plain_path(path)
    info = path.stat()
    if not stat.S_ISREG(info.st_mode):
        raise RuntimeError(f"Not a regular build file: {path}")
    return Build(path, info.st_size, info.st_mtime_ns, (info.st_dev, info.st_ino))


def active_builds():
    return {path for _, path in workbench_processes()}


def cleanup_plan(root=ROOT, keep=2):
    if keep < 1:
        raise ValueError("Keep at least one build per tool.")
    root = Path(os.path.abspath(root))
    plain_path(root)
    top = subprocess.run(["git", "-C", str(root), "rev-parse", "--show-toplevel"],
                         check=True, capture_output=True, text=True, encoding="utf-8").stdout.strip()
    if Path(top) != root or not (root / "cmd/ocr-web").is_dir():
        raise RuntimeError("Run this tool from the workbench Git repository.")
    dist = root / "dist"
    if not dist.exists():
        return []
    plain_path(dist)
    builds = []
    for path in dist.iterdir():
        if VARIANT.fullmatch(path.name) or path.name == "ocr-web.exe~":
            builds.append(snapshot(path))
    preserve = set()
    for tool in ("web", "repo"):
        variants = sorted((b for b in builds if b.path.name.startswith(f"ocr-{tool}-")),
                          key=lambda b: (-b.modified_ns, b.path.name))
        preserve.update(b.path for b in variants[:keep])
        backups = [b for b in variants if b.path.name.startswith(f"ocr-{tool}-before-")]
        preserve.update(b.path for b in backups[:1])
    tracked = set(git_files(root, untracked=False, paths=("dist/",)))
    preserve.update(active_builds())
    return sorted((b for b in builds if b.path not in preserve and
                   b.path.relative_to(root).as_posix() not in tracked), key=lambda b: b.path.name)


def apply_plan(plan, root=ROOT):
    removed = removed_bytes = 0
    for build in plan:
        if build.path.parent != root / "dist":
            raise RuntimeError(f"Not a direct build file: {build.path}")
        if snapshot(build.path) != build or build.path in active_builds():
            print(f"Preserved changed or active file: {build.path}")
            continue
        if git_files(root, untracked=False, paths=(build.path.relative_to(root).as_posix(),)):
            print(f"Preserved tracked file: {build.path}")
            continue
        build.path.unlink()
        removed += 1
        removed_bytes += build.size
    return removed, removed_bytes


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--apply", action="store_true", help="delete the listed old builds")
    parser.add_argument("--keep-per-tool", type=int, default=2)
    args = parser.parse_args(argv)
    plan = cleanup_plan(keep=args.keep_per_tool)
    for build in plan:
        print(f"{build.size / 2**20:8.2f} MiB  {build.path}")
    print(f"Selected: {len(plan)} files, {sum(b.size for b in plan) / 2**30:.2f} GiB.")
    if not args.apply:
        print("Preview only. Use --apply to delete these files.")
        return 0
    count, size = apply_plan(plan)
    print(f"Removed: {count} files, {size / 2**30:.2f} GiB.")
    return 0


if __name__ == "__main__":
    sys.exit(run_cli(main))
