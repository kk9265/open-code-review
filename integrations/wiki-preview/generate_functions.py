# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 alibaba/open-code-review Contributors

"""Run the explicitly scoped C++ class-guide pilot through the CodeWiki gateway."""

import asyncio
import hashlib
import json
import os
from pathlib import Path
import socket
import sys
import time
import warnings

from cpp_context import function_context, load_class
from function_guide import read_response, render, validate_class, validate_function, validate_review
from publish import atomic_json, publish

PROJECT = Path(__file__).resolve().parents[2]
COMMIT = "9052b9e77e681315a200a8489dfc6938d81024db"
PILOT = ("LoginServer", "LoginWhiteList", "LoginServer/LoginWhiteList.h", ["LoginServer/LoginWhiteList.cpp"])


def digest(value):
    return hashlib.sha256(json.dumps(value, ensure_ascii=True, sort_keys=True).encode()).hexdigest()


async def generate(context, gateway, directory, prompt, audit, concurrency, compatible=()):
    directory.mkdir(parents=True, exist_ok=True)
    semaphore = asyncio.Semaphore(concurrency)

    async def request(mode, payload, validate, name, feedback=None):
        request_body = {"mode": mode, "context": payload}
        if feedback:
            request_body["automated_feedback"] = feedback
        path = directory / (name + "-" + digest({"prompt": prompt, "request": request_body})[:16] + ".json")
        for candidate in [path, *(previous / path.name for previous in compatible)]:
            if candidate.exists():
                try:
                    value = validate(json.loads(candidate.read_text(encoding="utf-8")))
                except ValueError:
                    continue
                if candidate != path:
                    atomic_json(path, value)
                    audit({"event": "checkpoint_reused", "unit": name, "previous_run": candidate.parent.name})
                return value
        for attempt in range(2):
            async with semaphore:
                audit({"event": "request", "unit": name, "mode": mode, "attempt": attempt + 1})
                try:
                    response = await gateway.complete("page", [
                        {"role": "system", "content": prompt},
                        {"role": "user", "content": json.dumps(request_body, ensure_ascii=True)}
                    ], response_format="json_object")
                    audit({"event": "response", "unit": name, "model": response.model,
                           "usage": response.usage, "content_sha256": hashlib.sha256(response.content.encode()).hexdigest()})
                    value = validate(read_response(response.content))
                    atomic_json(path, value)
                    return value
                except Exception as exc:
                    audit({"event": "attempt_failed", "unit": name, "error_type": type(exc).__name__})
                    if attempt:
                        raise
                    request_body["validation_feedback"] = str(exc)[:1000]
            await asyncio.sleep(1)

    summary = await request("class", context, validate_class, "class")
    functions = {}

    async def explain(function, feedback=None):
        value = await request("function", function_context(context, function),
                              lambda result: validate_function(result, function), function["id"], feedback)
        functions[function["id"]] = value
        print(json.dumps({"event": "function_completed", "function": function["qualified_name"],
                          "completed": len(functions), "total": len(context["functions"])}), flush=True)

    await asyncio.gather(*(explain(function) for function in context["functions"]))
    for round_number in range(2):
        review = await request("review", {"source": context, "class_guide": summary, "functions": functions},
                               lambda value: validate_review(value, set(functions)), "review-" + str(round_number))
        if review["approved"]:
            return summary, functions, review
        if round_number:
            raise ValueError("Automated source review still has material findings; the pilot was not published")
        feedback = {}
        for finding in review["findings"]:
            feedback.setdefault(finding["function_id"], []).append(finding["issue"])
        if "class" in feedback:
            summary = await request("class", context, validate_class, "class", feedback["class"])
        await asyncio.gather(*(explain(function, feedback[function["id"]]) for function in context["functions"]
                               if function["id"] in feedback))
    raise RuntimeError("Missing automated source review")


def run(args):
    if getattr(args, "with_business", False):
        raise ValueError("--with-business is retired; use --remaining-modules for model-planned business groups")
    if (args.scope, args.class_name, args.header, args.implementation) != PILOT:
        raise ValueError("Only the LoginServer/LoginWhiteList pilot is enabled until user readability review")
    data = args.data_dir.resolve()
    source = data / "wiki-zgame-plan/zgame"
    context = load_class(source, COMMIT, args.scope, args.class_name, args.header, args.implementation)
    if args.dry_run:
        print(json.dumps({"scope": args.scope, "class_name": args.class_name, "functions": [
            {"id": f["id"], "key": f["key"], "declaration": f["declaration"], "implementation": f["implementation"]}
            for f in context["functions"]], "model_calls": 0}), flush=True)
        return
    output = data / "wiki-loginserver-generated"
    output.mkdir(exist_ok=True)
    manifest_path = output / "manifest.json"
    baseline = manifest_path.read_bytes() if manifest_path.exists() else None
    existing = json.loads(baseline) if baseline else {"pages": []}
    slug = "class-" + digest({"scope": args.scope, "class": args.class_name, "header": args.header})[:20]
    if any(page["slug"] != slug for page in existing["pages"]):
        raise ValueError("Pilot output contains other pages; explicit cleanup is required")
    if any(page.get("section_index") for page in existing["pages"]):
        raise ValueError("The published pilot includes business processes; scope generation is required to preserve them")
    prompt = (PROJECT / "integrations/wiki-preview/prompts/function-guide.md").read_text(encoding="utf-8")
    code_hashes = {name: hashlib.sha256((Path(__file__).parent / name).read_bytes()).hexdigest()
                   for name in ("cpp_context.py", "function_guide.py", "generate_functions.py")}
    selection = json.loads((output / "generation-model.json").read_text(encoding="utf-8"))
    if selection.get("source") != "aimana" or selection.get("model") not in {"glm-5.3", "deepseek-flash"}:
        raise ValueError("The pilot must use the configured AI Mana model")
    identity = digest({"context": context, "prompt": prompt, "model": selection, "code": code_hashes})
    directory = output / "pilot-runs" / identity
    lock = output / "function-guide.lock"
    descriptor = os.open(lock, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
            stream.write(str(os.getpid()))
        proxy = json.loads((Path(os.environ["LOCALAPPDATA"]) / ".aimana/proxy.json").read_text(encoding="utf-8-sig"))
        port = int(proxy["port"])
        if not 0 < port < 65536:
            raise ValueError("Invalid local proxy port")
        with socket.create_connection(("127.0.0.1", port), timeout=3):
            pass
        os.environ["LITELLM_LOCAL_MODEL_COST_MAP"] = "True"
        warnings.filterwarnings("ignore", category=SyntaxWarning)
        pinned = data / "wiki-code-plan-validation/pinned-release"
        from prepare import verify_checkout
        from worker import verify_lock
        verify_checkout(pinned, verify_lock())
        sys.path.insert(0, str(pinned))
        from backend.app.config import Settings, LLMSettings, LLMProfileSettings
        from backend.app.services.llm.gateway import LLMGateway
        settings = Settings(_env_file=None, storage_dir=output / "storage",
                            wiki_base_language="zh", llm=LLMSettings(
                                default=LLMProfileSettings(model="openai/" + selection["model"], provider_type="openai",
                                                           endpoint=f"http://127.0.0.1:{port}/v1", api_key="local-proxy",
                                                            max_tokens=6000),
                                timeout_seconds=300, max_retries=0, profiles={}))
        gateway = LLMGateway(settings)
        original_connect = socket.socket.connect

        def restricted_connect(sock, address):
            if not isinstance(address, tuple) or address[0] not in ("127.0.0.1", "::1"):
                raise RuntimeError("Only the AI Mana local proxy is permitted")
            return original_connect(sock, address)

        socket.socket.connect = restricted_connect
        directory.mkdir(parents=True, exist_ok=True)
        atomic_json(directory / "context.json", context)

        def audit(record):
            with (directory / "model-audit.jsonl").open("a", encoding="utf-8", newline="\n") as stream:
                stream.write(json.dumps({"time": time.time(), "source": "aimana", "model": selection["model"],
                                         "assistant_authorship": False, **record}, ensure_ascii=True) + "\n")

        try:
            compatible = []
            for previous in directory.parent.iterdir():
                context_file, audit_file = previous / "context.json", previous / "model-audit.jsonl"
                if previous == directory or not context_file.is_file() or not audit_file.is_file():
                    continue
                if json.loads(context_file.read_text(encoding="utf-8")) != context:
                    continue
                requests = [json.loads(line) for line in audit_file.read_text(encoding="utf-8").splitlines() if line.strip()]
                requests = [record for record in requests if record.get("event") == "request"]
                if requests and all(record.get("source") == "aimana" and record.get("model") == selection["model"] for record in requests):
                    compatible.append(previous)
            summary, functions, review = asyncio.run(
                generate(context, gateway, directory, prompt, audit, args.concurrency, compatible))
        finally:
            socket.socket.connect = original_connect
        labels = json.loads((PROJECT / "integrations/wiki-preview/ui/i18n/zh-CN.json").read_text(encoding="utf-8"))
        markdown, index = render(context, summary, functions, labels, slug)
        page = {"slug": slug, "title": args.class_name, "parent_slug": None, "page_type": "class",
                "markdown": markdown, "generation_model": selection["model"], "function_index": index,
                "section_index": [],
                "source_refs": [ref for function in context["functions"] for ref in
                                (function["declaration"], function["implementation"])]}
        if (manifest_path.read_bytes() if manifest_path.exists() else None) != baseline:
            raise ValueError("Published output changed during the pilot; refusing to replace it")
        progress = {"planned_pages": 1, "completed_pages": 1, "model": selection["model"],
                    "generated_models": {selection["model"]: 1}, "failures": {}, "function_count": len(index),
                    "review_status": "awaiting_user_review", "automated_source_review": review["approved"]}
        release = publish(output, {"repository": "zgame", "scope": args.scope, "commit": COMMIT,
                                   "mode": "function-guide", "review_status": "awaiting_user_review",
                                   "source": "aimana", "assistant_authorship": False, "pages": [page]}, progress)
        atomic_json(directory / "acceptance.json", {"class": args.class_name, "functions": len(index),
                                                   "complete_implementations": True, "automated_source_review": review,
                                                   "user_readability_review": "pending", "release": release})
        print(json.dumps({"event": "pilot_published", "scope": args.scope, "class_name": args.class_name,
                          "functions": len(index), "slug": slug, "review_status": "awaiting_user_review"}), flush=True)
    finally:
        lock.unlink(missing_ok=True)
