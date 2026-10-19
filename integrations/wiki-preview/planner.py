# SPDX-License-Identifier: Apache-2.0
from pathlib import PurePosixPath

from git_data import changes, tree


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
        "schema": 1,
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
            {"key": key, "reasons": sorted(set(value))}
            for key, value in sorted(reasons.items())
        ],
        "potentially_affected_pages": unknown,
        "unchanged_pages": [page["key"] for page in pages] if not delta else [],
        "catalog_review_directories": proposals,
        "catalog_required": not bool(pages),
        "impact_precision": "direct_source_and_parents; indirect_dependencies_unverified",
        "estimate": {
            "scheduled_model_calls": 0,
            "direct_page_count": len(reasons),
            "additional_pages_to_check": len(unknown),
            "tokens": None,
            "reason": "No generation request or model tokenizer is used in preview mode",
        },
    }
