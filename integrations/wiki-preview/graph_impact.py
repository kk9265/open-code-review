# SPDX-License-Identifier: Apache-2.0
import heapq
from collections import defaultdict


def impact(graph, touched):
    if graph.get("status") != "available":
        return {}, sorted(touched)
    nodes = graph["nodes"]
    reverse = defaultdict(list)
    for edge in graph["edges"]:
        source = nodes[edge["source_id"]]["file_path"]
        target = nodes[edge["target_id"]]["file_path"]
        if source != target:
            reverse[target].append((source, edge))
    roots = touched.intersection(graph["files"])
    best = {path: (0, 0) for path in roots}
    pending = [(0, 0, path) for path in sorted(roots)]
    heapq.heapify(pending)
    predecessor = {}
    while pending:
        uncertain, hops, target = heapq.heappop(pending)
        if best[target] != (uncertain, hops):
            continue
        for source, edge in reverse[target]:
            # Prefer a resolved chain over a shorter globally inferred chain.
            score = (max(uncertain, int(edge["confidence"] < 0.8)), hops + 1)
            if source in best and best[source] <= score:
                continue
            best[source] = score
            predecessor[source] = (target, edge)
            heapq.heappush(pending, (*score, source))
    paths = {}
    for path in sorted(predecessor):
        chain = []
        cursor = path
        while cursor not in roots:
            target, edge = predecessor[cursor]
            chain.append(
                {
                    "dependent_file": cursor,
                    "dependency_file": target,
                    "edge": edge,
                    "source": nodes[edge["source_id"]],
                    "target": nodes[edge["target_id"]],
                }
            )
            cursor = target
        paths[path] = {
            "file": path,
            "changed_file": cursor,
            "commit": graph["commit"],
            "graph_fingerprint": graph["fingerprint"],
            "resolution": "inferred" if best[path][0] else "resolved",
            "confidence": min(step["edge"]["confidence"] for step in chain),
            "chain": chain,
        }
    return paths, sorted(touched - roots)
