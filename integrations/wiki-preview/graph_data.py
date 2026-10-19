# SPDX-License-Identifier: Apache-2.0
import json
import math
import sqlite3
from contextlib import closing
from pathlib import Path

from git_data import blob_metadata, digest, tree

DEPENDENCIES = frozenset(
    {
        "calls",
        "references",
        "imports",
        "inherits",
        "implements",
        "routes_to",
        "uses_config",
    }
)


def freeze_graph(path, repo, sha):
    if path is None:
        return {"status": "unavailable", "reason": "not_supplied", "commit": sha}
    if sha is None:
        return {"status": "unavailable", "reason": "no_baseline", "commit": sha}
    try:
        graph = _read_graph(Path(path), repo, sha)
        graph["fingerprint"] = digest(graph)
        return graph
    except (OSError, sqlite3.Error, ValueError, TypeError, KeyError) as error:
        return {
            "status": "unavailable",
            "reason": "invalid_graph",
            "detail": str(error),
            "commit": sha,
        }


def _read_graph(path, repo, sha):
    # A read transaction keeps nodes, edges and run status in the same snapshot.
    with closing(sqlite3.connect(path.resolve().as_uri() + "?mode=ro", uri=True)) as db:
        db.row_factory = sqlite3.Row
        db.execute("BEGIN")
        repositories = db.execute(
            "SELECT id FROM repo WHERE commit_hash=?", (sha,)
        ).fetchall()
        if len(repositories) != 1:
            raise ValueError("Graph must contain exactly one repository at " + sha)
        repo_id = repositories[0]["id"]
        run = db.execute(
            "SELECT status FROM analysis_run WHERE repo_id=? "
            "ORDER BY started_at DESC, rowid DESC LIMIT 1",
            (repo_id,),
        ).fetchone()
        if run is None or run["status"] != "done":
            raise ValueError("Latest graph analysis is not complete")
        columns = {row[1] for row in db.execute("PRAGMA table_info(code_node)")}
        symbol_column = "symbol_id" if "symbol_id" in columns else "NULL AS symbol_id"
        raw_nodes = [
            dict(row)
            for row in db.execute(
                "SELECT id,type,name,file_path,start_line,end_line,hash,"
                + symbol_column
                + " "
                "FROM code_node WHERE repo_id=? ORDER BY id",
                (repo_id,),
            )
        ]
        raw_edges = [
            dict(row)
            for row in db.execute(
                "SELECT id,source_id,target_id,type,confidence,is_inferred,metadata_json "
                "FROM code_edge WHERE repo_id=? ORDER BY id",
                (repo_id,),
            )
        ]
    files = {
        node["file_path"]: node["hash"]
        for node in raw_nodes
        if node["type"] in {"file", "config"}
    }
    if not files:
        raise ValueError("Graph has no indexed files")
    entries = {
        entry["path"]: entry for entry in tree(repo, sha, []) if entry["kind"] == "blob"
    }
    if files.keys() - entries.keys():
        raise ValueError("Graph contains files outside the frozen Git tree")
    metadata = blob_metadata(repo, [entries[name] for name in sorted(files)])
    hashes = {path: value["sha256"] for path, value in metadata.items()}
    if hashes != files:
        raise ValueError("Graph file hashes differ from the frozen Git blobs")
    nodes = {}
    for node in raw_nodes:
        if node["type"] in {"repository", "directory", "module", "external"}:
            continue
        if node["file_path"] not in files:
            raise ValueError("Graph symbol has no validated file node")
        # CodeWiki parses UTF-8 text after universal-newline conversion.
        expected = metadata[node["file_path"]]
        if node["hash"] not in {expected["sha256"], expected["text_sha256"]}:
            raise ValueError("Graph symbol hash differs from its file node")
        nodes[node["id"]] = {key: value for key, value in node.items() if key != "hash"}
    raw_ids = {node["id"] for node in raw_nodes}
    edges = []
    for edge in raw_edges:
        if edge["source_id"] not in raw_ids or edge["target_id"] not in raw_ids:
            raise ValueError("Graph edge has a missing endpoint")
        if edge["type"] not in DEPENDENCIES:
            continue
        if edge["source_id"] not in nodes or edge["target_id"] not in nodes:
            continue
        confidence = float(edge["confidence"])
        if not math.isfinite(confidence) or not 0 <= confidence <= 1:
            raise ValueError("Graph edge confidence must be between zero and one")
        metadata = json.loads(edge.pop("metadata_json"))
        edge["confidence"] = confidence
        edge["is_inferred"] = bool(edge["is_inferred"])
        edge["evidence"] = {
            key: metadata[key]
            for key in (
                "reason",
                "resolution_tier",
                "call",
                "reference",
                "import",
                "base",
                "interface",
                "handler",
                "provenance",
            )
            if key in metadata
        }
        edges.append(edge)
    return {
        "status": "available",
        "commit": sha,
        "source_repo_id": repo_id,
        "files": sorted(files),
        "nodes": nodes,
        "edges": edges,
        "limitations": [
            "static_resolution_is_incomplete",
            "producer_version_unattested",
        ],
    }
