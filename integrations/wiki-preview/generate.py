# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 alibaba/open-code-review Contributors
import argparse
import asyncio
from collections import Counter
import hashlib
import json
import os
from pathlib import Path
import socket
import sqlite3
import sys
import time
import warnings

PROJECT = Path(__file__).resolve().parents[2]
parser = argparse.ArgumentParser(description="Generate and publish Chinese Wiki pages through AI Mana")
parser.add_argument("--scope", required=True, choices=("LoginServer", "AccountServer"))
parser.add_argument("--data-dir", type=Path, default=PROJECT / ".ocr-data")
parser.add_argument("--concurrency", type=int, choices=(1, 2, 3), default=2)
parser.add_argument("--repair-english", action="store_true", help="Regenerate only published English pages in Chinese")
parser.add_argument("--dry-run", action="store_true", help="Report language selection without loading models or changing data")
parser.add_argument("--resume-since", type=float, help="Reuse validated Chinese module results saved after this Unix timestamp")
parser.add_argument("--class-name", help="Generate the explicitly enabled C++ class-guide pilot")
parser.add_argument("--with-business", action="store_true", help=argparse.SUPPRESS)
parser.add_argument("--remaining-modules", action="store_true", help="Generate remaining LoginServer modules through AI Mana DeepSeek")
parser.add_argument("--header", help="Repository-relative class declaration file")
parser.add_argument("--implementation", action="append", help="Repository-relative implementation file")
parser.add_argument("--legacy-pages", action="store_true", help="Explicitly select the superseded file-page pipeline")
args = parser.parse_args()
if args.with_business:
    parser.error("--with-business is retired; use --remaining-modules for model-planned business groups")
if args.remaining_modules:
    if args.class_name or args.header or args.implementation or args.with_business or args.repair_english or args.resume_since is not None or args.legacy_pages:
        parser.error("Remaining modules cannot be combined with pilot or legacy modes")
    from generate_scope import run
    run(args)
    raise SystemExit(0)
if args.class_name:
    if args.repair_english or args.resume_since is not None or args.legacy_pages:
        parser.error("Class guides cannot be combined with legacy page modes")
    from generate_functions import run
    run(args)
    raise SystemExit(0)
if not args.legacy_pages and not args.dry_run:
    parser.error("Use an explicit --class-name pilot; bulk file generation is disabled pending readability review")
if args.dry_run and not args.repair_english:
    parser.error("--dry-run requires --repair-english")
if args.resume_since is not None and (not args.repair_english or not 0 < args.resume_since <= time.time()):
    parser.error("--resume-since requires --repair-english and a valid past timestamp")
DATA = args.data_dir.resolve()
SCOPE = args.scope
ROOT = DATA / ("wiki-" + SCOPE.lower() + "-plan")
PINNED = DATA / "wiki-code-plan-validation/pinned-release"
SOURCE = DATA / "wiki-zgame-plan/zgame"
OUTPUT = DATA / ("wiki-" + SCOPE.lower() + "-generated")
SHA = '9052b9e77e681315a200a8489dfc6938d81024db'
from catalog_filter import filter_items
from publish import atomic_json, publish
from repair_selection import prose_language, resumable_page, select_pages
published_path = OUTPUT / 'manifest.json'
published_body = published_path.read_bytes() if published_path.exists() else None
published = json.loads(published_body) if published_body is not None else {'pages': []}
selection_report = select_pages(published['pages']) if args.repair_english else None
if args.dry_run:
    print(json.dumps({'scope': SCOPE, 'mode': 'repair-english', 'selection': selection_report,
                      'counts': {key: len(value) for key, value in selection_report.items()}}))
    raise SystemExit(0)
selection_path = OUTPUT / 'generation-model.json'
selection = json.loads(selection_path.read_text(encoding='utf-8')) if selection_path.exists() else {'source': 'aimana', 'model': 'glm-5.3'}
assert selection['source'] == 'aimana'
MODEL = os.environ.get('WIKI_MODEL', selection['model'])
assert MODEL in {'glm-5.3', 'deepseek-flash'}
OUTPUT.mkdir(exist_ok=True)
os.environ['CODEWIKI_STORAGE_DIR'] = str(OUTPUT / 'storage')
os.environ['LITELLM_LOCAL_MODEL_COST_MAP'] = 'True'
os.environ['PYTHONUTF8'] = '1'
warnings.filterwarnings('ignore', category=SyntaxWarning)
sys.path.insert(0, str(PINNED))
sys.path.insert(0, str(PROJECT / 'integrations/wiki-preview'))
from catalog_preview import load_planner
load_planner(PINNED)

with sqlite3.connect((DATA / 'workbench.db').as_uri() + '?mode=ro', uri=True) as db:
    settings_body, settings_version = db.execute('SELECT payload,version FROM settings WHERE id=1').fetchone()
review = json.loads(settings_body)
assert review['model_source'] == 'aimana'
proxy = json.loads((Path(os.environ['LOCALAPPDATA']) / '.aimana/proxy.json').read_text(encoding='utf-8-sig'))
port = int(proxy['port'])
assert 0 < port < 65536
endpoint = f'http://127.0.0.1:{port}/v1'
original_connect = socket.socket.connect


def restricted_connect(sock, address):
    if not isinstance(address, tuple) or address[0] not in ('127.0.0.1', '::1'):
        raise RuntimeError('Only the AI Mana local proxy is permitted')
    return original_connect(sock, address)


socket.socket.connect = restricted_connect
from backend.app.config import Settings, LLMSettings, LLMProfileSettings
from backend.app.database import SQLiteStore
from backend.app.services.graphrag import GraphRAGRetriever
from backend.app.services.llm.gateway import LLMGateway
from backend.app.services.wiki.page_generator import WikiPageGenerator
from backend.app.services.wiki.tree import generation_nodes
import litellm

original_completion = litellm.acompletion


async def completion_with_low_reasoning(*args, **kwargs):
    if MODEL == 'glm-5.3':
        kwargs['extra_body'] = {**(kwargs.get('extra_body') or {}), 'reasoning_effort': 'low'}
    response = await original_completion(*args, **kwargs)
    audit({'event': 'provider_response', 'model': getattr(response, 'model', None)})
    return response


litellm.acompletion = completion_with_low_reasoning

database = OUTPUT / 'codewiki.sqlite3'
if not database.exists():
    with sqlite3.connect((DATA / 'wiki-zgame-plan/graph.sqlite3').as_uri() + '?mode=ro', uri=True) as source_db:
        with sqlite3.connect(database) as target_db:
            source_db.backup(target_db)
store = SQLiteStore(database)
store.ensure_schema()
repo = store.list_repos()[0]
assert repo.commit_hash == SHA and Path(repo.path).resolve() == SOURCE.resolve()
snapshot_path = next((ROOT / 'snapshots').glob('*/*.json'))
snapshot = json.loads(snapshot_path.read_text(encoding='utf-8'))
plan = snapshot.get('preview', snapshot)['code_plan']


def flatten(items):
    return [page for item in items for page in [item, *flatten(item.get('children', []))]]


file_roots = [item for item in flatten(plan['file_catalog']) if item['page_type'] == 'file']
items = filter_items(file_roots + plan['feature_catalog'])
nodes = generation_nodes(items)
assert file_roots and all(path.startswith(SCOPE + '/') for item in flatten(items) for path in item.get('source_hints', []))
if store.get_latest_doc_catalog(repo.id, language_code='zh') is None:
    store.save_doc_catalog(repo.id, title=SCOPE, structure={'items': items}, language_code='zh')
config = Settings(_env_file=None, storage_dir=OUTPUT / 'storage', wiki_base_language='zh',
                  llm=LLMSettings(default=LLMProfileSettings(model='openai/' + MODEL, provider_type='openai', endpoint=endpoint, api_key='local-proxy', max_tokens=int(os.environ.get('WIKI_MAX_TOKENS', '49152'))),
                                  timeout_seconds=600, max_retries=0, profiles={}))
audit_path = OUTPUT / 'model-audit.jsonl'


def audit(record):
    with audit_path.open('a', encoding='utf-8') as stream:
        stream.write(json.dumps({'time': time.time(), **record}) + '\n')


class AuditedGateway(LLMGateway):
    async def complete(self, task_type, messages, **kwargs):
        profile = self.router.profile_for(task_type)
        assert profile.model == 'openai/' + MODEL and profile.endpoint == endpoint
        audit({'event': 'request', 'task': task_type, 'source': 'aimana', 'model': profile.model})
        result = await super().complete(task_type, messages, **kwargs)
        audit({'event': 'response', 'task': task_type, 'model': result.model, 'usage': result.usage,
               'content_sha256': hashlib.sha256(result.content.encode()).hexdigest()})
        return result

    async def embed(self, *args, **kwargs):
        raise RuntimeError('Embedding model calls are not permitted')


gateway = AuditedGateway(config)
retriever = GraphRAGRetriever(store=store, settings=config)
generator = WikiPageGenerator(retriever, gateway, store=store)
results = {}
failures = {}
started = time.monotonic()
trial_limit = int(os.environ.get('WIKI_TRIAL_LIMIT', '0'))


previous_pages = {page['slug']: page for page in published['pages']}
planned = {node.slug: node for node in nodes}
new_pages = {}
repair_targets = set(selection_report['en']) if selection_report else set()
excluded_repairs = set()
if args.repair_english:
    excluded_slugs = {node.slug for node in generation_nodes(file_roots + plan['feature_catalog'])} - set(planned)
    excluded_repairs = repair_targets & excluded_slugs
    repair_targets -= excluded_repairs
    missing = repair_targets - set(planned)
    if missing:
        raise RuntimeError('English pages are absent from the filtered plan: ' + ', '.join(sorted(missing)))
required_slugs = set(previous_pages) if args.repair_english else set(planned)
if args.resume_since is not None:
    for page in store.list_doc_pages(repo.id, language_code='zh'):
        record = page.as_record_dict()
        if page.slug in repair_targets and resumable_page(record, args.resume_since):
            results[page.slug] = page
            new_pages[page.slug] = {**record, 'generation_model': MODEL, 'page_type': planned[page.slug].item['page_type']}
    audit({'event': 'repair_resume', 'since': args.resume_since, 'reused_pages': len(new_pages)})


def save_progress():
    pages = {**previous_pages, **new_pages}
    pages = {slug: page for slug, page in pages.items() if slug in required_slugs}
    remaining = len(repair_targets - set(new_pages)) if args.repair_english else len(nodes) - len(pages)
    progress = {'scope': SCOPE, 'commit': SHA, 'source': 'aimana', 'model': MODEL,
                'assistant_authorship': False, 'planned_pages': len(required_slugs), 'completed_pages': len(pages) - remaining if args.repair_english else len(pages),
                'generated_models': dict(Counter(page.get('generation_model', MODEL) for page in pages.values())),
                'statuses': {'generated': len(pages) - remaining if args.repair_english else len(pages), 'pending': remaining},
                'failures': failures, 'seconds': round(time.monotonic() - started, 1)}
    if args.repair_english:
        progress.update({'mode': 'repair-english', 'repair_targets': len(repair_targets),
                         'repaired_pages': len(new_pages), 'preserved_pages': len(previous_pages) - len(repair_targets),
                         'unknown_pages': selection_report['unknown'], 'excluded_script_pages': sorted(excluded_repairs)})
    atomic_json(OUTPUT / 'generation-progress.json', progress)
    if new_pages and not remaining and set(pages) == required_slugs and not failures:
        current_body = published_path.read_bytes() if published_path.exists() else None
        if current_body != published_body:
            raise RuntimeError('Published manifest changed during generation; refusing to overwrite it')
        publish(OUTPUT, {'repository': 'zgame', 'scope': SCOPE, 'commit': SHA,
                        'source': 'aimana', 'model': MODEL, 'assistant_authorship': False,
                        'pages': list(pages.values())}, progress)
    print(json.dumps(progress), flush=True)


async def main():
    pending = [node for node in nodes if node.slug in repair_targets and node.slug not in new_pages] if args.repair_english else [node for node in nodes if node.slug not in previous_pages]
    if not pending:
        if new_pages:
            save_progress()
            return
        print(json.dumps({'scope': SCOPE, 'new_pages': 0, 'existing_pages_unchanged': True, 'selection': selection_report}), flush=True)
        return
    if trial_limit:
        pending = pending[:trial_limit]
    if not store.list_code_chunks(repo.id):
        await retriever.build_index(repo.id, include_embeddings=False)
    semaphore = asyncio.Semaphore(args.concurrency)

    async def generate(node):
        async with semaphore:
            try:
                children = [results[child['slug']] for child in node.item.get('children', []) if child['slug'] in results]
                result = await generator.generate_page(repo.id, node.item, language_code='zh', parent_slug=node.parent_slug, child_pages=children)
                if result.validation_errors:
                    raise ValueError(' '.join(result.validation_errors))
                if args.repair_english and prose_language(result.page.markdown) != 'zh':
                    raise ValueError('Generated page did not pass the Chinese prose check')
                results[node.slug] = result.page
                new_pages[node.slug] = {**result.page.as_record_dict(), 'generation_model': MODEL, 'page_type': node.item['page_type']}
            except Exception as exc:
                failures[node.slug] = [str(exc)[:1000]]
            save_progress()

    for depth in sorted({node.depth for node in pending}, reverse=True):
        await asyncio.gather(*(generate(node) for node in pending if node.depth == depth))
    if failures or (repair_targets - set(new_pages) if args.repair_english else not set(planned).issubset(set(previous_pages) | set(new_pages))):
        raise SystemExit(2)


asyncio.run(main())
