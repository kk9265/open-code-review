# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 alibaba/open-code-review Contributors

"""Run model-only planning, source selection, explanations, reviews and assembly."""

import asyncio
from collections import Counter
import hashlib
import json
import re

from business_guide import render_business, validate_business_review, validate_process
from function_guide import prose, read_response, source_link, validate_function, validate_review
from generate_functions import digest
from publish import atomic_json


def title(value):
    prose(value, 40)
    if re.search(r"[\[\]{}*`#]", value):
        raise ValueError("Titles must be plain Chinese text")
    return value


def validate_plan(value, inventory):
    if not isinstance(value, dict) or set(value) != {"modules"} or not isinstance(value["modules"], list) or not 1 <= len(value["modules"]) <= 18:
        raise ValueError("Invalid module plan")
    known = {f["id"]: f for f in inventory["functions"]}
    assigned, assigned_files, titles = [], [], []
    for module in value["modules"]:
        if not isinstance(module, dict) or set(module) != {"title", "function_ids", "entry_ids", "files"}:
            raise ValueError("Invalid planned module fields")
        titles.append(title(module["title"]))
        ids, entries, files = module["function_ids"], module["entry_ids"], module["files"]
        if not isinstance(ids, list) or not ids or set(ids) - known.keys() or len(set(ids)) != len(ids):
            raise ValueError("Invalid module function assignment")
        if not isinstance(entries, list) or not 1 <= len(entries) <= 4 or set(entries) - set(ids):
            raise ValueError("Business entry identities must belong to their module")
        if not isinstance(files, list) or set(files) - set(inventory["files"]):
            raise ValueError("Unknown planned source file")
        if sum(len(known[i]["source"]) for i in ids) > 65000:
            raise ValueError("Planned module exceeds 65000 source characters; split function IDs: " + ",".join(ids))
        assigned.extend(ids)
        assigned_files.extend(files)
    if len(set(titles)) != len(titles):
        raise ValueError("Duplicate module titles")
    if len(assigned) != len(known) or set(assigned) != set(known):
        raise ValueError("Every function must appear exactly once; missing=" + ",".join(sorted(set(known) - set(assigned)))
                         + "; duplicated=" + ",".join(i for i, count in Counter(assigned).items() if count > 1))
    if set(assigned_files) != set(inventory["files"]):
        raise ValueError("Unassigned scope files: " + ",".join(sorted(set(inventory["files"]) - set(assigned_files))))
    return value


def validate_requests(value):
    if not isinstance(value, dict) or set(value) != {"requests"} or not isinstance(value["requests"], list) or len(value["requests"]) > 8:
        raise ValueError("Invalid source retrieval request")
    for request in value["requests"]:
        if not isinstance(request, dict) or set(request) != {"file_path", "symbol"}:
            raise ValueError("Invalid source request fields")
        if not isinstance(request["file_path"], str) or not isinstance(request["symbol"], str):
            raise ValueError("Source paths and exact symbol names must be strings")
    return value


def validate_batch(value, functions):
    if not isinstance(value, dict) or set(value) != {"functions"} or not isinstance(value["functions"], list):
        raise ValueError("Invalid function batch response")
    known = {f["id"]: f for f in functions}
    if len(value["functions"]) != len(known) or {f.get("id") for f in value["functions"]} != set(known):
        raise ValueError("Function batch must explain every target exactly once")
    checked, errors = [], []
    for function in value["functions"]:
        target = known[function["id"]]
        try:
            checked.append(validate_function(function, target))
        except (ValueError, TypeError) as exc:
            limits = target["explanation_limits"]
            sizes = {key: len(function[key]) if isinstance(function.get(key), (str, list, dict)) else None
                     for key in ("steps", "boundaries", "semantics")}
            lengths = {key: len(function[key]) if isinstance(function.get(key), str) else None
                       for key in ("index_summary", "summary")}
            lengths.update({key: [len(s) if isinstance(s, str) else None for s in function.get(key, [])]
                            for key in ("steps", "boundaries") if isinstance(function.get(key), list)})
            parts = [function.get("summary", "")]
            for key in ("steps", "boundaries"):
                if isinstance(function.get(key), list):
                    parts.extend(function[key])
            if isinstance(function.get("semantics"), dict):
                parts.extend(function["semantics"].values())
            lengths["total_prose"] = sum(len(re.sub(r"`[^`]+`", "", s)) for s in parts if isinstance(s, str))
            errors.append(target["id"] + ": " + str(exc) + "; actual_counts=" + json.dumps(sizes)
                          + "; actual_lengths=" + json.dumps(lengths) + "; required_limits=" + json.dumps(limits)
                          + "; field_maxima={index_summary:35,summary:120,step:120,boundary:140,semantic:160}")
    if errors:
        raise ValueError(" | ".join(errors))
    return {"functions": checked}


class ModelRequests:
    def __init__(self, gateway, directory, prompt, audit, concurrency, compatible=()):
        self.gateway, self.directory, self.prompt, self.audit = gateway, directory, prompt, audit
        self.semaphore = asyncio.Semaphore(concurrency)
        self.compatible = compatible

    async def request(self, mode, context, validator):
        body = {"mode": mode, "context": context}
        identity = digest({"prompt": self.prompt, "body": body})
        path = self.directory / (mode + "-" + identity[:24] + ".json")
        atomic_json(self.directory / (mode + "-" + identity[:24] + "-request.json"), body)
        for candidate in [path, *(p / path.name for p in self.compatible)]:
            if candidate.is_file():
                try:
                    result = validator(json.loads(candidate.read_text(encoding="utf-8")))
                except (ValueError, TypeError, KeyError):
                    continue
                if candidate != path:
                    atomic_json(path, result)
                    self.audit({"event": "checkpoint_reused", "mode": mode, "request": identity, "previous_run": candidate.parent.name})
                return result
        for attempt in range(3):
            async with self.semaphore:
                self.audit({"event": "request", "mode": mode, "request": identity, "attempt": attempt + 1})
                parsed = None
                try:
                    response = await self.gateway.complete("page", [{"role": "system", "content": self.prompt},
                        {"role": "user", "content": json.dumps(body, ensure_ascii=True)}], response_format="json_object")
                    self.audit({"event": "response", "mode": mode, "request": identity, "usage": response.usage,
                                "response_model": response.model, "content_sha256": hashlib.sha256(response.content.encode()).hexdigest()})
                    parsed = read_response(response.content)
                    value = validator(parsed)
                    atomic_json(path, value)
                    return value
                except (ValueError, TypeError, KeyError) as exc:
                    self.audit({"event": "validation_failed", "mode": mode, "request": identity,
                                "error_type": type(exc).__name__, "detail": str(exc)[:400]})
                    if attempt == 2:
                        raise
                    body["validation_feedback"] = str(exc)[:6000]
                    if mode == "functions":
                        body["strict_field_maxima"] = {"index_summary": 35, "summary": 120, "each_step": 120,
                                                       "each_boundary": 140, "each_semantic": 160}
                    if isinstance(parsed, dict) and len(json.dumps(parsed)) < 100000:
                        body["previous_result"] = parsed
                except Exception as exc:
                    self.audit({"event": "provider_failed", "mode": mode, "request": identity, "error_type": type(exc).__name__})
                    if attempt == 2:
                        raise
            await asyncio.sleep(1 + attempt)


async def explain_scope(sources, inventory, requests, progress, existing_coverage):
    compact = [{"id": f["id"], "name": f["name"], "key": f["key"], "reference": f["reference"],
                "source_characters": len(f["source"]), "calls": sorted({c["expression"] for c in f["calls"]})}
               for f in inventory["functions"]]
    plan = await requests.request("plan", {"scope": inventory["scope"], "commit": inventory["commit"],
        "functions": compact, "files": inventory["files"], "existing_coverage": existing_coverage},
        lambda value: validate_plan(value, inventory))
    atomic_json(requests.directory / "plan.json", plan)
    progress({"event": "scope_planned", "modules": len(plan["modules"]), "functions": len(compact)})
    known = {f["id"]: f for f in inventory["functions"]}
    modules, explanations, reviews = {}, {}, {}
    expected_modules = len(plan["modules"])
    failure_files = [p / "failures.json" for p in requests.compatible if (p / "failures.json").is_file()]
    format_failures = set()
    if failure_files:
        previous_failures = json.loads(max(failure_files, key=lambda p: p.stat().st_mtime).read_text(encoding="utf-8"))
        for failure in previous_failures["failures"]:
            if "prose budget" in failure or "actual_counts=" in failure:
                format_failures.update(re.findall(r"src-[a-f0-9]{16}", failure))

    def approved_business(identity, module):
        for previous in [requests.directory, *reversed(requests.compatible)]:
            for candidate in previous.glob("business-review-*-request.json"):
                body = json.loads(candidate.read_text(encoding="utf-8"))
                payload = body.get("context", {})
                group = payload.get("group", {})
                if payload.get("commit") != inventory["commit"] or group.get("id") != identity:
                    continue
                if set(group.get("required_callers", [])) != set(module["entry_ids"]):
                    continue
                units = {u["id"]: u for u in group.get("units", [])}
                if any(i not in units or units[i]["source"] != known[i]["source"] for i in module["function_ids"]):
                    continue
                result_path = candidate.with_name(candidate.name.replace("-request.json", ".json"))
                if not result_path.is_file():
                    continue
                review = validate_business_review(json.loads(result_path.read_text(encoding="utf-8")))
                if not review["approved"]:
                    continue
                original_request = {"prompt": requests.prompt, "body": body}
                if digest(original_request)[:24] not in candidate.name:
                    continue
                value = validate_process(payload["explanation"], group, payload["unavailable"])
                return group, {"units": group["units"], "unavailable": payload["unavailable"]}, value, review
        return None

    async def business(module, depth=0):
        nonlocal expected_modules
        identity = "process-" + digest(sorted(module["function_ids"]))[:20]
        saved = approved_business(identity, module)
        if saved:
            group, context, value, review = saved
            modules[identity] = (group, context, value)
            reviews[identity] = review
            atomic_json(requests.directory / (identity + "-context.json"), context)
            atomic_json(requests.directory / (identity + "-approved-result.json"), {"group": group, "explanation": value, "review": review})
            requests.audit({"event": "approved_module_reused", "unit": identity})
            progress({"event": "module_completed", "id": identity, "completed_modules": len(modules),
                      "planned_modules": expected_modules, "title": value["title"]})
            return
        context = sources.initial_context([known[i] for i in module["function_ids"]], module["files"])
        for round_number in range(2):
            selected = await requests.request("sources", {"title": module["title"], "scope": inventory["scope"],
                "commit": inventory["commit"], "round": round_number, **context}, validate_requests)
            if not selected["requests"]:
                break
            context = sources.fetch(selected["requests"], context)
        atomic_json(requests.directory / (identity + "-context.json"), context)
        group = {"id": identity, "required_callers": module["entry_ids"], "units": context["units"]}
        payload = {"scope": inventory["scope"], "commit": inventory["commit"], "planned_title": module["title"],
                   "group": group, "unavailable": context["unavailable"], "boundaries": context["boundaries"],
                   "resolution": context["resolution"], "existing_coverage": existing_coverage}
        validate = lambda value: validate_process(value, group, context["unavailable"])
        result = await requests.request("business", payload, validate)
        feedback = []
        review_rounds = 3 if depth == 0 else 4
        for round_number in range(review_rounds):
            review = await requests.request("business-review", {**payload, "explanation": result}, validate_business_review)
            if review["approved"]:
                modules[identity] = (group, context, result)
                reviews[identity] = review
                progress({"event": "module_completed", "id": identity, "completed_modules": len(modules),
                          "planned_modules": expected_modules, "title": result["title"]})
                return
            if round_number == review_rounds - 1:
                if depth == 0:
                    subset = {**inventory, "functions": [known[i] for i in module["function_ids"]], "files": module["files"]}

                    def validate_refinement(value):
                        value = validate_plan(value, subset)
                        if not 2 <= len(value["modules"]) <= 3:
                            raise ValueError("Refinement must split the responsibility into two or three narrower processes")
                        return value

                    refined = await requests.request("plan", {"scope": inventory["scope"], "commit": inventory["commit"],
                        "functions": [f for f in compact if f["id"] in module["function_ids"]], "files": module["files"],
                        "existing_coverage": existing_coverage, "refinement_of": module["title"],
                        "planning_constraint": "Split into two or three narrower business processes so each can explain its principal outcomes concisely. Preserve every assigned function exactly once. Do not write prose instead of planning.",
                        "review_feedback": [*feedback, *review["findings"]]}, validate_refinement)
                    atomic_json(requests.directory / (identity + "-refinement.json"), refined)
                    expected_modules += len(refined["modules"]) - 1
                    progress({"event": "module_refined", "id": identity, "planned_modules": expected_modules})
                    children = await asyncio.gather(*(business(child, depth + 1) for child in refined["modules"]),
                                                    return_exceptions=True)
                    errors = [child for child in children if isinstance(child, BaseException)]
                    if errors:
                        raise ValueError("Refined module checks failed: " + "; ".join(str(error) for error in errors))
                    return
                raise ValueError("Business source review failed after bounded model repairs: " + identity)
            feedback = list(dict.fromkeys([*feedback, *review["findings"]]))
            result = await requests.request("business", {**payload, "automated_feedback": feedback,
                "previous_explanation": result, "preserve_verified_behavior": True}, validate)

    async def functions(batch):
        if len(batch) > 1 and any(f["id"] in format_failures for f in batch):
            middle = len(batch) // 2
            progress({"event": "function_batch_split", "target_count": len(batch)})
            await asyncio.gather(functions(batch[:middle]), functions(batch[middle:]))
            return
        context = sources.initial_context(batch, [])
        payload = {"scope": inventory["scope"], "commit": inventory["commit"],
                   "strict_output_contract": {"targets": [{"id": f["id"], **f["explanation_limits"]} for f in batch],
                       "style": "Aim below half the prose budget. semantics=0 requires {}. Tiny functions need only one short step; omit redundant boundaries and all boilerplate. Use inline backticks for code identifiers."},
                   "targets": batch, **context}
        validate = lambda value: validate_batch(value, batch)
        if len(batch) == 1 and batch[0]["id"] in format_failures:
            target = {**batch[0], "explanation_limits": {**batch[0]["explanation_limits"],
                       "steps": min(3, batch[0]["explanation_limits"]["steps"]), "boundaries": 0, "semantics": 0}}
            payload.update({"targets": [target], "compact_output": True,
                "strict_output_contract": {"targets": [{"id": target["id"], **target["explanation_limits"]}],
                    "style": "Return a short summary and two or three short ordered steps within the supplied count limit. Each step must be at most 100 characters INCLUDING identifiers and backticks; split a long step instead of packing the whole function into it. Put every C++ identifier, type, comparison, SQL and macro expression inside inline backticks. Literal angle brackets outside backticks are forbidden. Backtick identifiers are excluded from the total prose budget; do not spell code identifiers in plain text. Put meaningful results/failures in summary or steps. boundaries must be [] and semantics must be {}. Do not repeat facts."}})

            def validate_compact(value):
                value = validate_batch(value, [target])
                maximum = min(120, target["explanation_limits"]["prose_characters"] // 2)
                prose(value["functions"][0]["summary"], maximum)
                prose(value["functions"][0]["steps"][0], maximum)
                return value

            validate = validate_compact
        try:
            result = await requests.request("functions", payload, validate)
        except (ValueError, TypeError, KeyError):
            if len(batch) == 1:
                if batch[0]["id"] not in format_failures:
                    format_failures.add(batch[0]["id"])
                    await functions(batch)
                    return
                raise
            middle = len(batch) // 2
            progress({"event": "function_batch_split", "target_count": len(batch)})
            await asyncio.gather(functions(batch[:middle]), functions(batch[middle:]))
            return
        feedback = []
        for round_number in range(3):
            review = await requests.request("function-review", {**payload, "explanations": result["functions"]},
                                            lambda value: validate_review(value, {f["id"] for f in batch}))
            if review["approved"]:
                explanations.update({f["id"]: f for f in result["functions"]})
                reviews["batch-" + digest([f["id"] for f in batch])[:16]] = review
                progress({"event": "functions_completed", "completed_functions": len(explanations),
                          "planned_functions": len(known)})
                return
            if round_number == 2:
                raise ValueError("Function source review failed after bounded model repairs")
            feedback.extend(f for f in review["findings"] if f not in feedback)
            result = await requests.request("functions", {**payload, "automated_feedback": feedback,
                "previous_explanations": result["functions"], "preserve_verified_behavior": True}, validate)

    # Source-budget batching preserves complete functions; the model owns semantic grouping.
    batches, batch, characters = [], [], 0
    for function in inventory["functions"]:
        size = len(function["source"])
        if batch and (len(batch) >= 6 or characters + size > 30000):
            batches.append(batch)
            batch, characters = [], 0
        batch.append(function)
        characters += size
    if batch:
        batches.append(batch)
    results = await asyncio.gather(*(business(module) for module in plan["modules"]),
                                   *(functions(batch) for batch in batches), return_exceptions=True)
    failures = [type(result).__name__ + ": " + str(result)[:500] for result in results if isinstance(result, BaseException)]
    if failures:
        atomic_json(requests.directory / "failures.json", {"failures": failures})
        raise ValueError("Scope generation is incomplete; published pages preserved. See failures.json")
    if set(explanations) != set(known) or len(modules) != expected_modules:
        raise ValueError("Scope coverage is incomplete")
    return plan, modules, explanations, reviews


def render_scope(inventory, modules, explanations, original, labels, model):
    from function_pages import partition_functions

    index_slug = "loginserver-function-index"
    pages = []
    owners = {}
    old_functions, old_body, old_refs = [], "", []
    for page in original["pages"]:
        if page["slug"] == index_slug:
            continue
        copied = dict(page)
        if copied.get("function_index"):
            marker = "## " + labels["guideFunctionIndex"]
            before, separator, after = copied["markdown"].partition(marker)
            if not separator or "## `" not in after:
                raise ValueError("Existing function section cannot be safely separated")
            offset = after.index("## `")
            moved = after[offset:].replace("page=" + page["slug"] + "&function=fn-", "page=" + index_slug + "&function=fn-")
            old_body += moved
            old_functions.extend(copied["function_index"])
            for entry in copied["function_index"]:
                owners[entry["id"]] = copied["title"]
            old_refs.extend(copied["source_refs"])
            copied.update({"markdown": before.rstrip() + "\n", "function_index": []})
        pages.append(copied)
    if not old_functions:
        raise ValueError("The existing pilot function results are required for scope expansion")
    for identity, (group, context, result) in sorted(modules.items()):
        slug = "module-" + identity.removeprefix("process-")
        body, _, refs = render_business({"groups": [group], "unavailable": context["unavailable"]},
                                        [(result, {"approved": True, "findings": []})], labels, slug)
        marker = "## " + result["title"] + " {#" + identity + "}"
        _, separator, content = body.partition(marker)
        if not separator:
            raise ValueError("Business renderer did not emit the expected section")
        pages.append({"slug": slug, "title": result["title"], "parent_slug": None, "page_type": "module",
            "markdown": "# " + result["title"] + " {#" + identity + "}\n" + content,
            "generation_model": model, "function_index": [], "section_index": [
                {"id": identity, "name": result["title"], "summary": result["summary"]}], "source_refs": refs})
    index = list(old_functions)
    sections, refs = [], list(old_refs)
    for function in inventory["functions"]:
        owners[function["id"]] = function["name"].rsplit("::", 1)[0] if "::" in function["name"] else function["reference"]["file_path"]
        value = validate_function(explanations[function["id"]], function)
        signature = function["key"].replace("`", "")
        index.append({"id": function["id"], "name": function["name"], "signature": signature, "summary": value["index_summary"]})
        sections += ["## `" + signature + "` {#" + function["id"] + "}", value["summary"],
                     source_link(function["reference"], index_slug, function["id"], labels["guideImplementation"]),
                     "\n".join(str(i + 1) + ". " + step for i, step in enumerate(value["steps"]))]
        refs.append(function["reference"])
        if value["boundaries"]:
            sections += ["**" + labels["guideBoundaries"] + "**", "\n".join("- " + text for text in value["boundaries"])]
        for key, text in value["semantics"].items():
            sections.append("**" + labels["guideSemantic_" + key] + "**: " + text)
    markdown = "# " + labels["guideFunctionIndex"] + "\n\n"
    markdown += "\n".join("- [`" + entry["signature"].replace("`", "") + "`](#" + entry["id"] + ") — " + entry["summary"]
                           for entry in index) + "\n\n" + old_body + "\n\n" + "\n\n".join(sections) + "\n"
    pages.append({"slug": index_slug, "title": labels["guideFunctionIndex"], "parent_slug": None,
                  "page_type": "function-index", "markdown": markdown, "generation_model": model,
                  "function_index": index, "section_index": [], "source_refs": refs})
    return partition_functions(pages, lambda entry, references: owners[entry["id"]])
