# SPDX-License-Identifier: Apache-2.0
from pathlib import PurePosixPath

from git_data import changes, tree
from graph_impact import impact


def preview(inputs):
    repo, head, base = inputs["repo_path"], inputs["commit"], inputs["baseline"]
    delta = changes(repo, base, head, inputs["scope"])
    files = tree(repo, head, inputs["scope"])
    touched = {
        p for item in delta for p in [item["path"], item.get("old_path", item["path"])]
    }
    pages = inputs["catalog"]
    reasons = {}
    for page in pages:
        hits = sorted(touched.intersection(page["source_files"]))
        if hits:
            reasons[page["key"]] = ["source_changed:" + path for path in hits]
        elif touched and page["global"]:
            reasons[page["key"]] = ["global_summary"]
    by_key = {page["key"]: page for page in pages}
    direct = set(reasons)
    graph_status = {}
    dependency_paths = {}
    for side in ("baseline", "target"):
        graph = inputs.get("graphs", {}).get(
            side, {"status": "unavailable", "reason": "not_supplied"}
        )
        roots = {
            item.get("old_path", item["path"]) if side == "baseline" else item["path"]
            for item in delta
            if (side == "baseline" and item["status"][0] != "A")
            or (side == "target" and item["status"][0] != "D")
        }
        paths, missing = impact(graph, roots)
        graph_status[side] = {
            key: value
            for key, value in graph.items()
            if key not in {"nodes", "edges", "files"}
        }
        graph_status[side]["unindexed_changed_files"] = missing
        graph_status[side]["indexed_file_count"] = len(graph.get("files", []))
        for path, evidence in paths.items():
            dependency_paths.setdefault(path, []).append({"side": side, **evidence})
    indirect = {}
    for page in pages:
        evidence = [
            item
            for path in page["source_files"]
            for item in dependency_paths.get(path, [])
        ]
        if evidence:
            indirect[page["key"]] = evidence
            reasons.setdefault(page["key"], []).extend(
                "dependency_changed:" + item["file"] for item in evidence
            )
    for key in list(reasons):
        seen = {key}
        parent = by_key[key]["parent"]
        while parent in by_key and parent not in seen:
            seen.add(parent)
            reasons.setdefault(parent, []).append("child_affected:" + key)
            parent = by_key[parent]["parent"]
    proposals = sorted(
        {
            str(PurePosixPath(item["path"]).parent)
            for item in delta
            if item["status"][0] in {"A", "R", "C"}
        }
    )
    unknown = (
        [page["key"] for page in pages if page["key"] not in reasons] if delta else []
    )
    return {
        "schema": 2,
        "mode": "preview_only",
        "generation_enabled": False,
        "model_calls": 0,
        "repo_id": inputs["repo_id"],
        "branch": inputs["branch"],
        "commit": head,
        "baseline": base,
        "config_fingerprint": inputs["config_fingerprint"],
        "changed_files": delta,
        "source_tree": files,
        "affected_pages": [
            {
                "key": key,
                "reasons": sorted(set(value)),
                "impact": "direct"
                if key in direct
                else "indirect"
                if key in indirect
                else "parent",
                "dependency_evidence": indirect.get(key, []),
            }
            for key, value in sorted(reasons.items())
        ],
        "potentially_affected_pages": unknown,
        "unknown_page_details": [
            {"key": key, "reason": "no_static_dependency_path; safe_reuse_unverified"}
            for key in unknown
        ],
        "graphs": graph_status,
        "unchanged_pages": [page["key"] for page in pages] if not delta else [],
        "catalog_review_directories": proposals,
        "catalog_required": not bool(pages),
        "impact_precision": "file_level_static_dependency_candidates; absence_is_not_safe_reuse",
        "estimate": {
            "scheduled_model_calls": 0,
            "direct_page_count": len(direct),
            "indirect_page_count": len(set(indirect) - direct),
            "parent_page_count": len(set(reasons) - direct - set(indirect)),
            "additional_pages_to_check": len(unknown),
            "tokens": None,
            "reason": "No generation request or model tokenizer is used in preview mode",
        },
    }
