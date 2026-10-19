# SPDX-License-Identifier: Apache-2.0
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path

from git_data import (
    blob_metadata,
    commit,
    digest,
    git,
    repository,
    scope_paths,
    tree,
)
from graph_data import freeze_graph
from prepare import verify_checkout
from store import Store
from worker import HERE, verify_lock

EXTENSION = "backend/app/services/wiki/catalog/code_plan.py"


def load_planner(source):
    lock = verify_lock()
    verify_checkout(source, lock)
    spec = importlib.util.spec_from_file_location(
        "codewiki_catalog_plan", source / EXTENSION
    )
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module.build_code_plan


def build_preview(
    repo_path,
    branch,
    graph_path,
    source,
    target=None,
    scope=(),
    large_lines=300,
    large_symbols=12,
    feature_limit=80,
):
    source = Path(source).resolve()
    planner = load_planner(source)
    repo, repo_id = repository(repo_path)
    git(repo, "check-ref-format", "--branch", branch)
    branch_head = commit(repo, "refs/heads/" + branch)
    sha = commit(repo, target) if target else branch_head
    git(repo, "merge-base", "--is-ancestor", sha, branch_head)
    paths = scope_paths(scope)
    graph = freeze_graph(graph_path, repo, sha)
    if graph["status"] != "available":
        raise ValueError(
            "An exact, complete CodeWiki graph is required: " + json.dumps(graph)
        )
    entries = [item for item in tree(repo, sha, paths) if item["kind"] == "blob"]
    metadata = blob_metadata(repo, entries)
    files = [
        {"path": path, "line_count": item["line_count"]}
        for path, item in metadata.items()
    ]
    plan = planner(
        list(graph["nodes"].values()),
        graph["edges"],
        files,
        large_lines=large_lines,
        large_symbols=large_symbols,
        feature_limit=feature_limit,
    )
    fingerprint = digest(
        {
            "graph": graph["fingerprint"],
            "scope": paths,
            "large_lines": large_lines,
            "large_symbols": large_symbols,
            "feature_limit": feature_limit,
            "lock": verify_lock(),
            "adapter": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
            "git_data": hashlib.sha256((HERE / "git_data.py").read_bytes()).hexdigest(),
        }
    )
    inputs = {
        "repo_path": str(repo),
        "repo_id": repo_id,
        "branch": branch,
        "commit": sha,
        "config_fingerprint": fingerprint,
        "scope": paths,
        "catalog": [],
        "plan_kind": "code_catalog",
    }
    payload = {
        "schema": 3,
        "mode": "preview_only",
        "plan_kind": "code_catalog",
        "generation_enabled": False,
        "model_calls": 0,
        "wiki_writes": 0,
        "repo_id": repo_id,
        "branch": branch,
        "commit": sha,
        "baseline": None,
        "config_fingerprint": fingerprint,
        "code_plan": plan,
        "scope": paths,
        "unindexed_files": [
            item["path"] for item in files if item["path"] not in graph["files"]
        ],
        "producer": {
            "codewiki_commit": verify_lock()["commit"],
            "extension": EXTENSION,
        },
        "graph": {
            key: value
            for key, value in graph.items()
            if key not in {"nodes", "edges", "files"}
        },
        "thresholds": {"large_lines": large_lines, "large_symbols": large_symbols},
    }
    return inputs, payload


def main():
    parser = argparse.ArgumentParser(
        description="CodeWiki file/feature catalog planning; no generation"
    )
    parser.add_argument("--repo", required=True)
    parser.add_argument("--branch", required=True)
    parser.add_argument("--commit")
    parser.add_argument("--graph", type=Path, required=True)
    parser.add_argument("--codewiki-source", type=Path, required=True)
    parser.add_argument("--scope", action="append", default=[])
    parser.add_argument("--large-lines", type=int, default=300)
    parser.add_argument("--large-symbols", type=int, default=12)
    parser.add_argument("--feature-limit", type=int, default=80)
    parser.add_argument(
        "--state", type=Path, default=HERE.parents[1] / ".ocr-data/wiki-code-plan"
    )
    args = parser.parse_args()
    inputs, payload = build_preview(
        args.repo,
        args.branch,
        args.graph,
        args.codewiki_source,
        args.commit,
        args.scope,
        args.large_lines,
        args.large_symbols,
        args.feature_limit,
    )
    store = Store(args.state)
    job, reused = store.enqueue(inputs)
    if job["status"] != "succeeded":
        claimed = store.claim(job_id=job["id"])
        if claimed is None:
            raise RuntimeError("Requested plan is already running")
        store.finish(claimed, payload)
    print(
        json.dumps(
            {
                "snapshot_id": job["fingerprint"],
                "deduplicated": reused,
                "file_count": payload["code_plan"]["file_count"],
                "feature_count": payload["code_plan"]["feature_count"],
                "responsibility_page_count": payload["code_plan"][
                    "responsibility_page_count"
                ],
                "model_calls": 0,
                "wiki_writes": 0,
            }
        )
    )


if __name__ == "__main__":
    main()
