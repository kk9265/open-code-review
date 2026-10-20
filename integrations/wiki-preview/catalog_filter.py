# SPDX-License-Identifier: Apache-2.0
"""Exclude operational scripts before Wiki planning and generation."""

from pathlib import PurePosixPath

POLICY_VERSION = "exclude-operational-scripts-v1"
SCRIPT_SUFFIXES = {".sh", ".bash", ".bat", ".cmd", ".ps1", ".psm1"}
ENTRY_NAMES = {"start", "stop", "restart", "startup", "shutdown", "start_server", "stop_server", "restart_server"}


def excluded_path(path):
    path = PurePosixPath(path.replace("\\", "/").lower())
    return path.suffix in SCRIPT_SUFFIXES or (path.suffix == ".py" and path.stem in ENTRY_NAMES)


def filter_items(items):
    result = []
    for item in items:
        hints = item.get("source_hints", [])
        kept = [path for path in hints if not excluded_path(path)]
        if (hints and not kept) or excluded_path(item.get("entry_symbol", {}).get("file_path", "")):
            continue
        children = filter_items(item.get("children", []))
        if item.get("kind") == "directory" and not children:
            continue
        result.append({**item, "source_hints": kept, "children": children})
    return result


def filtered_plan(planner, nodes, edges, files, **kwargs):
    excluded = [item["path"] for item in files if excluded_path(item["path"])]
    plan = planner(nodes, edges, [item for item in files if not excluded_path(item["path"])], **kwargs)
    return {**plan, "excluded_files": sorted(set(plan.get("excluded_files", [])) | set(excluded)), "file_filter": POLICY_VERSION}
