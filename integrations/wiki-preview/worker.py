# SPDX-License-Identifier: Apache-2.0
import argparse
import hashlib
import json
import sys
from pathlib import Path

from git_data import catalog, commit, digest, git, repository, scope_paths
from graph_data import freeze_graph
from planner import preview
from store import Store

HERE = Path(__file__).resolve().parent


def verify_lock():
    lock = json.loads((HERE / "vendor-lock.json").read_text(encoding="utf-8"))
    for name, expected in lock["artifacts"].items():
        if hashlib.sha256((HERE / name).read_bytes()).hexdigest() != expected:
            raise ValueError("Pinned artifact changed: " + name)
    return lock


def runtime_fingerprint():
    implementation = {
        name: hashlib.sha256((HERE / name).read_bytes()).hexdigest()
        for name in [
            "worker.py",
            "planner.py",
            "git_data.py",
            "graph_data.py",
            "graph_impact.py",
        ]
    }
    return digest({"lock": verify_lock(), "implementation": implementation})


def make_inputs(
    repo_path,
    branch,
    target=None,
    baseline=None,
    manifest=None,
    scope=(),
    baseline_graph=None,
    target_graph=None,
):
    repo, repo_id = repository(repo_path)
    git(repo, "check-ref-format", "--branch", branch)
    branch_head = commit(repo, "refs/heads/" + branch)
    head = commit(repo, target) if target else branch_head
    git(repo, "merge-base", "--is-ancestor", head, branch_head)
    base = commit(repo, baseline) if baseline else None
    pages = catalog(manifest, base)
    paths = scope_paths(scope)
    graphs = {
        "baseline": freeze_graph(baseline_graph, repo, base),
        "target": freeze_graph(target_graph, repo, head),
    }
    runtime = runtime_fingerprint()
    fingerprint = digest(
        {
            "runtime": runtime,
            "scope": paths,
            "catalog": pages,
            "baseline": base,
            "graphs": graphs,
        }
    )
    return {
        "repo_path": str(repo),
        "repo_id": repo_id,
        "branch": branch,
        "commit": head,
        "baseline": base,
        "scope": paths,
        "catalog": pages,
        "graphs": graphs,
        "config_fingerprint": fingerprint,
        "runtime_fingerprint": runtime,
    }


def work_once(store, build=preview, lease_seconds=300):
    job = store.claim(lease_seconds)
    if job is None:
        return {"status": "idle"}
    try:
        inputs = json.loads(job["inputs"])
        if inputs["runtime_fingerprint"] != runtime_fingerprint():
            raise RuntimeError("Preview implementation changed; enqueue a new request")
        payload = build(inputs)
        snapshot = store.finish(job, payload)
        return {"status": "succeeded", "job_id": job["id"], "snapshot_id": snapshot}
    except Exception as error:  # noqa: BLE001 - Persist failures at the worker boundary.
        store.fail(job, error)
        return {"status": "failed", "job_id": job["id"], "error": str(error)}


def main(argv=None):
    parser = argparse.ArgumentParser(
        description="Offline Wiki preview queue; generation is unavailable"
    )
    parser.add_argument(
        "--state", type=Path, default=HERE.parents[1] / ".ocr-data/wiki-preview"
    )
    commands = parser.add_subparsers(dest="command", required=True)
    enqueue = commands.add_parser("enqueue")
    enqueue.add_argument("--repo", required=True)
    enqueue.add_argument("--branch", required=True)
    enqueue.add_argument("--commit")
    enqueue.add_argument("--baseline")
    enqueue.add_argument("--catalog", type=Path)
    enqueue.add_argument("--scope", action="append", default=[])
    enqueue.add_argument("--baseline-graph", type=Path)
    enqueue.add_argument("--target-graph", type=Path)
    commands.add_parser("work-once")
    commands.add_parser("jobs")
    commands.add_parser("snapshots")
    commands.add_parser("current")
    show = commands.add_parser("show")
    show.add_argument("snapshot_id")
    args = parser.parse_args(argv)
    try:
        verify_lock()
        store = Store(args.state)
        if args.command == "enqueue":
            inputs = make_inputs(
                args.repo,
                args.branch,
                args.commit,
                args.baseline,
                args.catalog,
                args.scope,
                args.baseline_graph,
                args.target_graph,
            )
            job, reused = store.enqueue(inputs)
            result = {
                "job_id": job["id"],
                "status": job["status"],
                "deduplicated": reused,
            }
        elif args.command == "work-once":
            result = work_once(store)
        elif args.command == "show":
            result = store.read_snapshot(args.snapshot_id)
        else:
            result = store.list(
                {
                    "jobs": "jobs",
                    "snapshots": "snapshots",
                    "current": "current_previews",
                }[args.command]
            )
        print(json.dumps(result, ensure_ascii=True, indent=2))
        return 1 if isinstance(result, dict) and result.get("status") == "failed" else 0
    except Exception as error:  # noqa: BLE001 - Keep CLI failures machine-readable.
        print(json.dumps({"error": str(error)}, ensure_ascii=True), file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
