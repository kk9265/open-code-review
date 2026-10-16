# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 alibaba/open-code-review Contributors

"""Optional context sidecar. It never opens the workbench database."""

import argparse
from contextlib import closing
import hashlib
import json
import os
from pathlib import Path
import re
import sqlite3
import subprocess
import threading
import time
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


class FileLock:
    def __init__(self, path):
        path.parent.mkdir(parents=True, exist_ok=True)
        self.file = path.open("a+b")
        self.file.seek(0, 2)
        if self.file.tell() == 0:
            self.file.write(b"0")
            self.file.flush()
        self.file.seek(0)
        try:
            if os.name == "nt":
                import msvcrt
                msvcrt.locking(self.file.fileno(), msvcrt.LK_NBLCK, 1)
            else:
                import fcntl
                fcntl.flock(self.file, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except OSError:
            self.file.close()
            raise ValueError("Repository context is in use; retry after the active review ends.")

    def close(self):
        self.file.close()


def read_json(path):
    return json.loads(path.read_text(encoding="utf-8-sig"))


def write_json(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    pending = path.with_suffix(".tmp")
    pending.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n", encoding="utf-8", newline="\n")
    pending.replace(path)


def execute(argv, cwd=None, timeout=300):
    env = os.environ.copy()
    env.update({"GIT_CONFIG_GLOBAL": os.devnull, "GIT_CONFIG_NOSYSTEM": "1", "GIT_TERMINAL_PROMPT": "0",
                "CODEGRAPH_NO_DAEMON": "1", "CODEGRAPH_NO_WATCH": "1", "DO_NOT_TRACK": "1", "CODEGRAPH_NO_UPDATE_CHECK": "1"})
    flags = subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0
    result = subprocess.run(argv, cwd=cwd, env=env, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, timeout=timeout, creationflags=flags)
    if result.returncode:
        detail = result.stderr.decode("utf-8", errors="replace")[-2000:]
        raise ValueError(f"{Path(argv[0]).name} {argv[1]} failed: {detail}")
    return result.stdout.decode("utf-8", errors="replace")


class Service:
    def __init__(self, config_path):
        self.config_path = config_path.resolve()
        self.config = read_json(self.config_path)
        self.root = (self.config_path.parent / self.config["data_dir"]).resolve()
        self.root.mkdir(parents=True, exist_ok=True)
        self.sessions = {}
        self.guard = threading.Lock()

    def repo(self, repo_id):
        key = str(repo_id)
        if not re.fullmatch(r"[1-9][0-9]*", key) or key not in self.config.get("repositories", {}):
            raise ValueError("Repository is not registered with this context service.")
        return key, self.config["repositories"][key]

    def command(self):
        command = self.config.get("codegraph_command", [])
        if not isinstance(command, list) or not command or not all(isinstance(x, str) for x in command):
            raise ValueError("Configure codegraph_command as an executable argument array.")
        return command

    def check_index(self, work, revision):
        database = work / ".codegraph" / "codegraph.db"
        with closing(sqlite3.connect(database.resolve().as_uri() + "?mode=ro", uri=True)) as db:
            metadata = dict(db.execute("SELECT key, value FROM project_metadata"))
        if metadata.get("index_state") != "complete" or metadata.get("indexed_at_commit") != revision:
            raise ValueError("CodeGraph has not published a complete index for this commit.")
        dirty = json.loads(metadata.get("indexed_dirty_paths", "{}"))
        if dirty.get("paths"):
            raise ValueError("CodeGraph indexed uncommitted changes; rebuild from the frozen commit.")

    def complete_index(self, work):
        database = work / ".codegraph" / "codegraph.db"
        try:
            with closing(sqlite3.connect(database.resolve().as_uri() + "?mode=ro", uri=True)) as db:
                return db.execute("SELECT value FROM project_metadata WHERE key='index_state'").fetchone() == ("complete",)
        except sqlite3.Error:
            return False

    def prepare(self, key, repo, revision):
        if not re.fullmatch(r"[0-9a-f]{40}|[0-9a-f]{64}", revision):
            raise ValueError("An exact commit SHA is required.")
        source = Path(repo["source"]).resolve(strict=True)
        base = self.root / "repositories" / key
        work = base / "checkout"
        state_path = base / "index.json"
        git = ["git", "-c", "core.autocrlf=false", "-c", "core.hooksPath=" + str(self.root / "empty-hooks")]
        if execute(git + ["-C", str(source), "rev-parse", "--verify", revision + "^{commit}"]).strip() != revision:
            raise ValueError("Requested commit is unavailable in the registered source.")
        if work.exists() and not state_path.exists():
            raise ValueError("Unowned checkout found; refusing to modify it.")
        state = read_json(state_path) if state_path.exists() else {}
        if state and state.get("source") != str(source):
            raise ValueError("Registered source changed; use a new context data directory.")
        if state.get("revision") == revision and state.get("ready") and (work / ".codegraph").is_dir():
            if execute(git + ["-C", str(work), "rev-parse", "HEAD"]).strip() == revision:
                execute(git + ["-C", str(work), "diff", "--exit-code", "HEAD", "--"])
                self.check_index(work, revision)
                return work
        write_json(state_path, {"source": str(source), "revision": revision, "ready": False})
        if not work.exists():
            execute(git + ["clone", "--no-hardlinks", "--no-checkout", "--", str(source), str(work)])
        else:
            execute(git + ["-C", str(work), "fetch", "--no-tags", "origin", revision])
        execute(git + ["-C", str(work), "checkout", "--detach", "--force", revision, "--"])
        command = self.command()
        timeout = self.config.get("index_timeout_seconds", 1800)
        if not isinstance(timeout, int) or timeout < 1:
            raise ValueError("index_timeout_seconds must be a positive integer.")
        if (work / ".codegraph").is_dir():
            action = ["sync", str(work)] if self.complete_index(work) else ["index", str(work), "--force"]
            execute(command + action, cwd=work, timeout=timeout)
        else:
            execute(command + ["init", str(work), "--yes"], cwd=work, timeout=timeout)
        if execute(git + ["-C", str(work), "rev-parse", "HEAD"]).strip() != revision:
            raise ValueError("Checkout revision changed during indexing.")
        execute(git + ["-C", str(work), "diff", "--exit-code", "HEAD", "--"])
        self.check_index(work, revision)
        write_json(state_path, {"source": str(source), "revision": revision, "ready": True, "updated_at": time.time()})
        return work

    def wiki_snapshot(self, key, branch):
        scope = hashlib.sha256(branch.encode("utf-8")).hexdigest()[:16]
        root = self.root / "wiki" / key / scope
        articles = []
        size = 0
        if root.exists():
            for path in sorted(root.rglob("*.md")):
                if not path.resolve().is_relative_to(root.resolve()) or path.is_symlink():
                    continue
                # Only explicitly approved articles enter a review snapshot.
                metadata = path.with_suffix(".json")
                if not metadata.is_file() or metadata.is_symlink():
                    continue
                meta = read_json(metadata)
                if meta.get("approved") is not True or meta.get("branch") != branch or not meta.get("source"):
                    continue
                if path.stat().st_size > 256 * 1024:
                    raise ValueError("Wiki article exceeds 256 KiB; split it before publishing.")
                text = path.read_text(encoding="utf-8-sig")
                size += len(text.encode("utf-8"))
                if size > 8 * 1024 * 1024:
                    raise ValueError("Wiki snapshot exceeds 8 MiB; narrow the repository scope.")
                articles.append({"path": str(path.relative_to(root)), "source": meta["source"],
                                 "applicability": meta.get("applicability", "Not specified; verify before relying on it."),
                                 "updated_at": meta.get("updated_at", "unknown"),
                                 "sha256": hashlib.sha256(text.encode("utf-8")).hexdigest(), "text": text})
        return articles

    def expire(self):
        now = time.monotonic()
        with self.guard:
            for key, session in list(self.sessions.items()):
                if session["expires"] < now and session["guard"].acquire(blocking=False):
                    session["lock"].close()
                    session["guard"].release()
                    del self.sessions[key]

    def open(self, data):
        self.expire()
        key, repo = self.repo(data.get("repository_id"))
        revision, branch = data.get("revision", ""), data.get("branch", "")
        if not isinstance(branch, str) or len(branch) > 1024 or not isinstance(revision, str) or not re.fullmatch(r"[0-9a-f]{40}|[0-9a-f]{64}", revision):
            raise ValueError("Invalid frozen review scope.")
        lock = FileLock(self.root / "repositories" / key / "context.lock")
        try:
            capabilities, notices = [], []
            work = None
            if repo.get("codegraph", False):
                try:
                    work = self.prepare(key, repo, revision)
                    capabilities.append("code")
                    notices.append("Code index pinned to " + revision + ". Graph edges may be incomplete; absence is not proof.")
                except (OSError, ValueError, sqlite3.Error, subprocess.TimeoutExpired) as exc:
                    notices.append("Code index unavailable: " + str(exc))
            articles = self.wiki_snapshot(key, branch) if repo.get("wiki", False) else []
            if articles:
                capabilities.append("wiki")
                notices.append(f"Wiki snapshot: {len(articles)} approved articles for branch {branch}; verify each article's applicability.")
            else:
                notices.append("No approved Wiki articles are available for this branch.")
            session_id = uuid.uuid4().hex
            with self.guard:
                self.sessions[session_id] = {"revision": revision, "work": work, "articles": articles,
                                             "capabilities": capabilities, "lock": lock, "guard": threading.Lock(),
                                             "expires": time.monotonic() + 20 * 60}
            return {"session_id": session_id, "revision": revision, "capabilities": capabilities, "notice": "\n".join(notices)}
        except Exception:
            lock.close()
            raise

    def query(self, data):
        self.expire()
        with self.guard:
            session = self.sessions.get(data.get("session_id"))
            if session is None:
                raise ValueError("Context session expired or is unknown.")
            if not session["guard"].acquire(blocking=False):
                raise ValueError("A context query is already running.")
        try:
            query = data.get("query", "")
            capability = data.get("capability")
            if not isinstance(query, str) or not query.strip() or len(query.encode("utf-8")) > 4000 or query.startswith("-"):
                raise ValueError("Provide a nonempty query up to 4000 bytes, without leading command options.")
            if capability not in session["capabilities"]:
                raise ValueError("Capability is not available in this session.")
            if capability == "code":
                work = session["work"]
                head = execute(["git", "-C", str(work), "rev-parse", "HEAD"]).strip()
                if head != session["revision"]:
                    raise ValueError("Indexed checkout revision changed; refusing mixed-version context.")
                execute(["git", "-C", str(work), "diff", "--exit-code", "HEAD", "--"])
                self.check_index(work, session["revision"])
                text = execute(self.command() + ["explore", query], cwd=work, timeout=75)
                self.check_index(work, session["revision"])
            else:
                words = re.findall(r"[a-zA-Z0-9_]+|[\u3400-\u9fff]{1,}", query.lower())
                words += [word[i:i+2] for word in words[:] if re.search(r"[\u3400-\u9fff]", word) for i in range(len(word)-1)]
                ranked = []
                for article in session["articles"]:
                    haystack = (article["path"] + "\n" + article["text"]).lower()
                    score = sum(haystack.count(word) for word in set(words))
                    if score:
                        ranked.append((score, article))
                ranked.sort(key=lambda item: (-item[0], item[1]["path"]))
                text = json.dumps([article for _, article in ranked[:5]], ensure_ascii=False) if ranked else "No matching approved articles in this branch snapshot."
            if len(text.encode("utf-8")) > 22000:
                text = text.encode("utf-8")[:22000].decode("utf-8", errors="ignore") + "\nIS_TRUNCATED: true; narrow the query."
            return {"revision": session["revision"], "text": text}
        finally:
            session["guard"].release()

    def close(self, data):
        with self.guard:
            key = data.get("session_id")
            session = self.sessions.get(key)
            if session:
                if not session["guard"].acquire(blocking=False):
                    raise ValueError("Wait for the active query before closing its session.")
                session["lock"].close()
                session["guard"].release()
                del self.sessions[key]
        return {"closed": True}


def serve(service, port):
    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def do_POST(self):
            status = 200
            try:
                if self.headers.get("Origin") or self.headers.get("Content-Type") != "application/json":
                    raise ValueError("Only local application JSON requests are accepted.")
                length = int(self.headers.get("Content-Length", "0"))
                if not 0 < length <= 16384:
                    raise ValueError("Invalid request size.")
                self.connection.settimeout(10)
                data = json.loads(self.rfile.read(length))
                if not isinstance(data, dict):
                    raise ValueError("Expected a JSON object.")
                action = {"/v1/open": service.open, "/v1/query": service.query, "/v1/close": service.close}.get(self.path)
                if action is None:
                    raise ValueError("Unknown read-only context endpoint.")
                result = action(data)
            except (ValueError, OSError, sqlite3.Error, subprocess.TimeoutExpired) as exc:
                status, result = 409, {"error": str(exc)}
            body = json.dumps(result, ensure_ascii=False).encode("utf-8")
            try:
                self.send_response(status)
                self.send_header("Content-Type", "application/json; charset=utf-8")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)
            except (BrokenPipeError, ConnectionResetError, TimeoutError):
                if self.path == "/v1/open" and status == 200:
                    service.close(result)

    server = ThreadingHTTPServer(("127.0.0.1", port), Handler)
    def reap():
        while True:
            time.sleep(5)
            service.expire()
    threading.Thread(target=reap, daemon=True).start()
    print(f"Context service listening on http://127.0.0.1:{port}", flush=True)
    server.serve_forever()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", required=True, type=Path)
    sub = parser.add_subparsers(dest="action", required=True)
    start = sub.add_parser("serve")
    start.add_argument("--port", type=int, default=5487)
    index = sub.add_parser("index")
    index.add_argument("--repository", required=True)
    index.add_argument("--revision", required=True)
    wiki = sub.add_parser("wiki-path")
    wiki.add_argument("--repository", required=True)
    wiki.add_argument("--branch", required=True)
    args = parser.parse_args()
    service = Service(args.config)
    if args.action == "serve":
        serve(service, args.port)
    elif args.action == "index":
        key, repo = service.repo(args.repository)
        lock = FileLock(service.root / "repositories" / key / "context.lock")
        try:
            print(service.prepare(key, repo, args.revision))
        finally:
            lock.close()
    else:
        key, _ = service.repo(args.repository)
        scope = hashlib.sha256(args.branch.encode("utf-8")).hexdigest()[:16]
        print(service.root / "wiki" / key / scope)


if __name__ == "__main__":
    main()
