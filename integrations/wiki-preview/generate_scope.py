# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 alibaba/open-code-review Contributors

"""Execute authorized remaining-scope generation exclusively through AI Mana DS."""

import asyncio
import hashlib
import json
import os
from pathlib import Path
import socket
import sys
import time
import warnings

from generate_functions import COMMIT, PROJECT, digest
from publish import atomic_json, publish
from scope_context import ScopeSources
from scope_guide import ModelRequests, explain_scope, render_scope


def run(args):
    if args.scope != "LoginServer":
        raise ValueError("Only the remaining LoginServer scope is authorized")
    data = args.data_dir.resolve()
    sources = ScopeSources(data / "wiki-zgame-plan/zgame", COMMIT, args.scope)
    inventory = sources.inventory()
    if args.dry_run:
        print(json.dumps({"scope": args.scope, "commit": COMMIT, "remaining_functions": len(inventory["functions"]),
            "files": inventory["files"], "parser_uncertainties": [f["reference"] for f in inventory["functions"] if f["parse_error"]],
            "excluded_files": inventory["excluded_files"], "model_calls": 0}), flush=True)
        return
    output = data / "wiki-loginserver-generated"
    manifest_path = output / "manifest.json"
    baseline = manifest_path.read_bytes()
    original = json.loads(baseline)
    if original.get("scope_generation", {}).get("commit") == COMMIT:
        print(json.dumps({"event": "scope_already_completed", "pages": len(original["pages"])}), flush=True)
        return
    if original.get("commit") != COMMIT or len(original["pages"]) != 1 or original["pages"][0]["title"] != "LoginWhiteList":
        raise ValueError("Scope expansion requires the preserved LoginWhiteList pilot at the same commit")
    selection = json.loads((output / "generation-model.json").read_text(encoding="utf-8"))
    if selection.get("source") != "aimana" or selection.get("model") != "deepseek-flash":
        raise ValueError("This execution must use the configured AI Mana DeepSeek model")
    prompt = (Path(__file__).parent / "prompts/scope-guide.md").read_text(encoding="utf-8")
    code = {name: hashlib.sha256((Path(__file__).parent / name).read_bytes()).hexdigest()
            for name in ("scope_context.py", "scope_guide.py", "generate_scope.py")}
    identity = digest({"sources": inventory["source_hashes"], "prompt": prompt, "selection": selection,
                       "code": code, "published": hashlib.sha256(baseline).hexdigest()})
    directory = output / "scope-runs" / identity
    lock = output / "function-guide.lock"
    descriptor = os.open(lock, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    original_connect = socket.socket.connect
    original_completion = None
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
            stream.write(str(os.getpid()))
        directory.mkdir(parents=True, exist_ok=True)
        atomic_json(directory / "inventory.json", inventory)
        atomic_json(directory / "run-meta.json", {"selection": selection, "commit": COMMIT, "code": code,
                                                   "assistant_authorship": False, "reasoning_effort": "low"})
        compatible = []
        for previous in directory.parent.iterdir():
            meta = previous / "run-meta.json"
            if previous != directory and meta.is_file():
                value = json.loads(meta.read_text(encoding="utf-8"))
                if value.get("selection") == selection and value.get("commit") == COMMIT and value.get("assistant_authorship") is False:
                    compatible.append(previous)

        def audit(record):
            policy = ({"thinking": "disabled", "reasoning_effort": None} if record.get("mode") in {"plan", "functions"}
                      else {"thinking": "enabled", "reasoning_effort": "low"})
            with (directory / "model-audit.jsonl").open("a", encoding="utf-8", newline="\n") as stream:
                stream.write(json.dumps({"time": time.time(), "source": "aimana", "model": selection["model"],
                    **policy, "assistant_authorship": False, **record}, ensure_ascii=True) + "\n")

        state = {"scope": args.scope, "source": "aimana", "model": selection["model"], "assistant_authorship": False,
                 "status": "running", "planned_functions": len(inventory["functions"]), "completed_functions": 0,
                 "completed_modules": 0, "run": identity}

        def progress(record):
            state.update({k: v for k, v in record.items() if k != "event"})
            atomic_json(directory / "progress.json", state)
            atomic_json(output / "generation-progress.json", state)
            print(json.dumps(record, ensure_ascii=True), flush=True)

        proxy = json.loads((Path(os.environ["LOCALAPPDATA"]) / ".aimana/proxy.json").read_text(encoding="utf-8-sig"))
        port = int(proxy["port"])
        if not 0 < port < 65536:
            raise ValueError("Invalid local AI Mana proxy port")
        with socket.create_connection(("127.0.0.1", port), timeout=3):
            pass
        os.environ["LITELLM_LOCAL_MODEL_COST_MAP"] = "True"
        warnings.filterwarnings("ignore", category=SyntaxWarning)
        from prepare import verify_checkout
        from worker import verify_lock
        pinned = data / "wiki-code-plan-validation/pinned-release"
        verify_checkout(pinned, verify_lock())
        sys.path.insert(0, str(pinned))
        from backend.app.config import Settings, LLMSettings, LLMProfileSettings
        from backend.app.services.llm.gateway import LLMGateway
        import litellm
        original_completion = litellm.acompletion

        async def completion(*call_args, **kwargs):
            mode = json.loads(kwargs["messages"][-1]["content"]).get("mode")
            if mode in {"plan", "functions"}:
                kwargs.pop("reasoning_effort", None)
                kwargs["extra_body"] = {**kwargs.get("extra_body", {}), "thinking": {"type": "disabled"}}
                kwargs["max_tokens"] = 16000
            else:
                kwargs["reasoning_effort"] = "low"
            return await original_completion(*call_args, **kwargs)

        def restricted_connect(sock, address):
            if not isinstance(address, tuple) or address[0] not in ("127.0.0.1", "::1"):
                raise RuntimeError("Only the local AI Mana proxy is permitted")
            return original_connect(sock, address)

        litellm.acompletion = completion
        socket.socket.connect = restricted_connect
        settings = Settings(_env_file=None, storage_dir=output / "storage", wiki_base_language="zh",
            llm=LLMSettings(default=LLMProfileSettings(model="openai/" + selection["model"], provider_type="openai",
                endpoint=f"http://127.0.0.1:{port}/v1", api_key="local-proxy", max_tokens=12000),
                timeout_seconds=300, max_retries=0, profiles={}))
        requests = ModelRequests(LLMGateway(settings), directory, prompt, audit, args.concurrency, compatible)
        existing_coverage = [{"title": page["title"], "processes": [s["name"] for s in page.get("section_index", [])]}
                             for page in original["pages"]]
        try:
            plan, modules, explanations, reviews = asyncio.run(explain_scope(sources, inventory, requests, progress, existing_coverage))
        except Exception as exc:
            progress({"event": "scope_failed", "status": "failed", "error_type": type(exc).__name__})
            raise
        finally:
            socket.socket.connect = original_connect
            litellm.acompletion = original_completion
        labels = json.loads((Path(__file__).parent / "ui/i18n/zh-CN.json").read_text(encoding="utf-8"))
        pages = render_scope(inventory, modules, explanations, original, labels, selection["model"])
        if manifest_path.read_bytes() != baseline:
            raise ValueError("Published pages changed during generation; refusing to overwrite")
        coverage = {"commit": COMMIT, "remaining_functions": len(explanations), "modules": len(modules),
                    "files": inventory["files"], "excluded_files": inventory["excluded_files"], "run": identity,
                    "source_hashes": inventory["source_hashes"], "user_readability_review": "pending"}
        release = publish(output, {"repository": "zgame", "scope": args.scope, "commit": COMMIT,
            "mode": "function-guide", "review_status": "awaiting_user_review", "source": "aimana",
            "assistant_authorship": False, "scope_generation": coverage, "pages": pages},
            {**state, "status": "completed", "planned_pages": len(pages), "failures": {},
             "review_status": "awaiting_user_review", "automated_source_review": True})
        atomic_json(directory / "acceptance.json", {**coverage, "release": release, "reviews": reviews,
                                                   "assistant_authorship": False})
        progress({"event": "scope_published", "status": "completed", "pages": len(pages), "release": release})
    finally:
        socket.socket.connect = original_connect
        if original_completion is not None:
            import litellm
            litellm.acompletion = original_completion
        lock.unlink(missing_ok=True)
