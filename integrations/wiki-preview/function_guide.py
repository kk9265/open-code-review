# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 alibaba/open-code-review Contributors

"""Validate and assemble model-authored class guides without writing explanatory prose."""

import json
import re
from urllib.parse import quote, urlencode

SEMANTICS = ("preconditions", "parameters", "returns", "state_changes", "ownership", "timing",
             "concurrency", "retry", "dependencies", "build_conditions", "unknowns")


def prose(value, limit):
    if not isinstance(value, str) or not value.strip() or len(value) > limit:
        raise ValueError("Missing or oversized explanatory text")
    if not re.search(r"[\u3400-\u9fff]", value):
        raise ValueError("Explanatory text must be Chinese")
    plain = re.sub(r"`[^`\n]*`", "", value)
    if re.search(r"```|~~~|\[\[|https?://|[<>]|\]\(|(?m:^\s*#)|\n", plain):
        raise ValueError("Text contains unsupported layout or links")
    return value.strip()


def items(value, count, limit, minimum=0):
    if not isinstance(value, list) or not minimum <= len(value) <= count:
        raise ValueError("Invalid item count")
    return [prose(item, limit) for item in value]


def validate_class(result):
    if not isinstance(result, dict) or set(result) != {"summary", "shared_notes"}:
        raise ValueError("Invalid class response fields")
    return {"summary": prose(result["summary"], 100), "shared_notes": items(result["shared_notes"], 2, 120)}


def validate_function(result, function):
    if not isinstance(result, dict) or set(result) != {"id", "index_summary", "summary", "steps", "boundaries", "semantics"}:
        raise ValueError("Invalid function response fields")
    if result["id"] != function["id"]:
        raise ValueError("Function identity mismatch")
    semantics = result["semantics"]
    limits = function.get("explanation_limits", {"prose_characters": 600, "steps": 6, "boundaries": 4, "semantics": 2})
    if not isinstance(semantics, dict) or len(semantics) > limits["semantics"] or set(semantics) - set(SEMANTICS):
        raise ValueError("Invalid optional semantics")
    validated = {"id": result["id"], "index_summary": prose(result["index_summary"], 35), "summary": prose(result["summary"], 120),
                 "steps": items(result["steps"], limits["steps"], 120, 1),
                 "boundaries": items(result["boundaries"], limits["boundaries"], 140),
                 "semantics": {key: prose(value, 160) for key, value in semantics.items()}}
    parts = [validated["summary"], *validated["steps"], *validated["boundaries"], *validated["semantics"].values()]
    size = sum(len(re.sub(r"`[^`]+`", "", part)) for part in parts)
    if size > limits["prose_characters"]:
        raise ValueError("Function explanation exceeds its complexity-based prose budget")
    return validated


def validate_review(result, function_ids):
    if not isinstance(result, dict) or set(result) != {"approved", "findings"} or type(result["approved"]) is not bool:
        raise ValueError("Invalid automated review")
    if not isinstance(result["findings"], list) or len(result["findings"]) > 20:
        raise ValueError("Invalid review findings")
    for finding in result["findings"]:
        if not isinstance(finding, dict) or set(finding) != {"function_id", "issue"}:
            raise ValueError("Invalid review finding fields")
        if finding["function_id"] not in {"class", *function_ids}:
            raise ValueError("Review refers to a function outside the pilot")
        prose(finding["issue"], 500)
    if result["approved"] != (not result["findings"]):
        raise ValueError("Review approval contradicts its findings")
    return result


def source_link(ref, slug, anchor, label):
    url = "/wiki/read/source/" + quote(ref["file_path"], safe="/")
    url += "?" + urlencode({"page": slug, "function": anchor})
    url += "#L" + str(ref["start_line"]) + "-L" + str(ref["end_line"])
    return "[" + label + "](" + url + ")"


def render(context, summary, functions, labels, slug):
    expected = {function["id"] for function in context["functions"]}
    if set(functions) != expected:
        raise ValueError("Every declared function must have exactly one explanation")
    summary = validate_class(summary)
    functions = {function["id"]: validate_function(functions[function["id"]], function)
                 for function in context["functions"]}
    sections = ["# " + context["class_name"], summary["summary"]]
    if summary["shared_notes"]:
        sections += ["\n".join("- " + note for note in summary["shared_notes"])]
    sections += ["## " + labels["guideFunctionIndex"]]
    index = []
    for function in context["functions"]:
        explanation = functions[function["id"]]
        signature = function["signature"]
        if "`" in signature or "\n" in signature:
            signature = " ".join(signature.replace("`", "").split())
        sections += ["- [`" + signature + "`](#" + function["id"] + ") — " + explanation["index_summary"]]
        index.append({"id": function["id"], "name": function["name"], "signature": signature,
                      "summary": explanation["summary"]})
    for function in context["functions"]:
        explanation = functions[function["id"]]
        signature = next(entry["signature"] for entry in index if entry["id"] == function["id"])
        sections += ["## `" + signature + "` {#" + function["id"] + "}", explanation["summary"],
                     source_link(function["declaration"], slug, function["id"], labels["guideDeclaration"])
                     + " · " + source_link(function["implementation"], slug, function["id"], labels["guideImplementation"]),
                     "\n".join(str(i + 1) + ". " + step for i, step in enumerate(explanation["steps"]))]
        if explanation["boundaries"]:
            sections += ["**" + labels["guideBoundaries"] + "**", "\n".join("- " + item for item in explanation["boundaries"])]
        for key, value in explanation["semantics"].items():
            sections += ["**" + labels["guideSemantic_" + key] + "**: " + value]
    return "\n\n".join(sections) + "\n", index


def read_response(content):
    value = json.loads(content)
    if not isinstance(value, dict):
        raise ValueError("The model must return one JSON object")
    return value
