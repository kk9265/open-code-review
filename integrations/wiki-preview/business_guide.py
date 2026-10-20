# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 alibaba/open-code-review Contributors

"""Generate source-reviewed business explanations and assemble model text verbatim."""

import asyncio
import hashlib
import json
import re

from function_guide import prose, read_response, source_link
from publish import atomic_json


def validate_process(value, group, unavailable):
    if not isinstance(value, dict) or set(value) != {"id", "title", "summary", "relationships", "steps", "outcomes", "gaps"}:
        raise ValueError("Invalid business process fields")
    if value["id"] != group["id"]:
        raise ValueError("Business process identity mismatch")
    prose(value["title"], 30)
    if re.search(r"[\[\]{}*`#]", value["title"]):
        raise ValueError("A process title must be plain text")
    prose(value["summary"], 140)
    known = {unit["id"] for unit in group["units"]}
    missing = {unit["id"] for unit in unavailable}
    cited = set()
    for field, minimum, maximum in [("relationships", 1, 2), ("steps", 2, 6), ("outcomes", 1, 2), ("gaps", 0, 2)]:
        entries = value[field]
        if not isinstance(entries, list) or not minimum <= len(entries) <= maximum:
            raise ValueError("Invalid business item count: " + field)
        for entry in entries:
            fields = {"text", "source_ids"} | ({"kind", "missing_ids"} if field == "gaps" else set())
            if not isinstance(entry, dict) or set(entry) != fields:
                raise ValueError("Invalid business item fields: " + field)
            prose(entry["text"], 180)
            refs = entry["source_ids"]
            if not isinstance(refs, list) or not 1 <= len(refs) <= 5 or len(set(refs)) != len(refs) or set(refs) - known:
                raise ValueError("Unknown, duplicate or missing business source identity")
            cited.update(refs)
            if field == "gaps":
                if entry["kind"] not in {"missing_source", "runtime_boundary", "not_included"}:
                    raise ValueError("Invalid gap kind")
                ids = entry["missing_ids"]
                if not isinstance(ids, list) or len(set(ids)) != len(ids) or set(ids) - missing:
                    raise ValueError("Unverified missing source claim")
                if (entry["kind"] == "missing_source") != bool(ids):
                    raise ValueError("Missing source gaps require a verified absent include")
    if set(group["required_callers"]) - cited:
        raise ValueError("Business explanation omits a direct whitelist caller")
    text = [value["summary"], *(e["text"] for f in ("relationships", "steps", "outcomes", "gaps") for e in value[f])]
    if sum(len(re.sub(r"`[^`]+`", "", s)) for s in text) > 800:
        raise ValueError("Business explanation exceeds its prose budget")
    return value


def validate_business_review(value):
    if not isinstance(value, dict) or set(value) != {"approved", "findings"} or type(value["approved"]) is not bool:
        raise ValueError("Invalid business source review")
    if not isinstance(value["findings"], list) or len(value["findings"]) > 12:
        raise ValueError("Invalid business review findings")
    for finding in value["findings"]:
        prose(finding, 600)
    if value["approved"] != (not value["findings"]):
        raise ValueError("Business review contradicts its findings")
    return value


async def generate_business(context, gateway, directory, prompt, audit, concurrency, compatible=()):
    semaphore = asyncio.Semaphore(concurrency)

    async def request(mode, payload, validator):
        body = {"mode": mode, "context": payload}
        identity = hashlib.sha256(json.dumps({"prompt": prompt, "request": body}, sort_keys=True).encode()).hexdigest()
        path = directory / ("business-" + identity[:24] + ".json")
        for candidate in [path, *(previous / path.name for previous in compatible)]:
            if candidate.exists():
                try:
                    value = validator(json.loads(candidate.read_text(encoding="utf-8")))
                except ValueError:
                    continue
                if candidate != path:
                    atomic_json(path, value)
                    audit({"event": "checkpoint_reused", "unit": payload["group"]["id"],
                           "mode": mode, "previous_run": candidate.parent.name})
                return value
        for attempt in range(2):
            async with semaphore:
                audit({"event": "request", "unit": payload["group"]["id"], "mode": mode, "attempt": attempt + 1})
                response = await gateway.complete("page", [{"role": "system", "content": prompt},
                    {"role": "user", "content": json.dumps(body, ensure_ascii=True)}], response_format="json_object")
                audit({"event": "response", "unit": payload["group"]["id"], "mode": mode,
                       "model": response.model, "usage": response.usage,
                       "content_sha256": hashlib.sha256(response.content.encode()).hexdigest()})
                try:
                    value = validator(read_response(response.content))
                    atomic_json(path, value)
                    return value
                except ValueError as exc:
                    audit({"event": "validation_failed", "unit": payload["group"]["id"],
                           "error_type": type(exc).__name__, "detail": str(exc)[:300]})
                    if attempt:
                        raise
                    body["validation_feedback"] = str(exc)

    async def explain(group):
        payload = {"class_name": context["class_name"], "commit": context["commit"], "group": group,
                   "related_processes": [{"id": g["id"], "source_names": [u["name"] for u in g["units"]]}
                                         for g in context["groups"] if g["id"] != group["id"]],
                   "unavailable": context["unavailable"], "resolution": context["resolution"]}
        validator = lambda result: validate_process(result, group, context["unavailable"])
        value = await request("process", payload, validator)
        feedback = []
        for round_number in range(3):
            review = await request("review", {**payload, "explanation": value}, validate_business_review)
            if review["approved"]:
                print(json.dumps({"event": "business_process_completed", "id": group["id"]}), flush=True)
                return value, review
            if round_number == 2:
                raise ValueError("Business source review still has material findings; refusing publication")
            feedback = list(dict.fromkeys([*feedback, *review["findings"]]))
            repair = {**payload, "automated_feedback": feedback}
            if round_number:
                repair.update({"previous_explanation": value, "preserve_verified_behavior": True})
            value = await request("process", repair, validator)
        raise RuntimeError("Missing business source review")

    return await asyncio.gather(*(explain(group) for group in context["groups"]))


def render_business(context, results, labels, slug):
    if len(results) != len(context["groups"]):
        raise ValueError("Incomplete business process coverage")
    sections = ["## " + labels["guideBusinessIndex"] + " {#business-index}"]
    index, refs = [], []
    for group, (value, review) in zip(context["groups"], results):
        validate_process(value, group, context["unavailable"])
        if not validate_business_review(review)["approved"]:
            raise ValueError("Unapproved business explanation")
        sections.append("- [" + value["title"] + "](#" + group["id"] + ")")
        index.append({"id": group["id"], "name": value["title"], "summary": value["summary"]})
    for group, (value, _) in zip(context["groups"], results):
        units = {u["id"]: u for u in group["units"]}
        sections += ["## " + value["title"] + " {#" + group["id"] + "}", value["summary"]]
        for field, label in [("relationships", "guideRelationships"), ("steps", "guideBusinessSteps"),
                             ("outcomes", "guideBusinessOutcomes"), ("gaps", "guideBusinessGaps")]:
            if not value[field]:
                continue
            sections.append("**" + labels[label] + "**")
            lines = []
            for number, item in enumerate(value[field], 1):
                links = []
                for identity in item["source_ids"]:
                    unit = units[identity]
                    refs.append(unit["reference"])
                    links.append(source_link(unit["reference"], slug, group["id"], "`" + unit["name"] + "`"))
                prefix = str(number) + ". " if field == "steps" else "- "
                # One compact source link per item; all supplied references remain allowlisted.
                lines.append(prefix + item["text"] + " " + links[0])
            sections.append("\n".join(lines))
    return "\n\n".join(sections) + "\n", index, list({json.dumps(r, sort_keys=True): r for r in refs}.values())
