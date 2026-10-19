# SPDX-License-Identifier: Apache-2.0
import argparse
import hashlib
import json
import re
import sqlite3
from contextlib import closing
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlsplit

HERE = Path(__file__).resolve().parent
ASSETS = {
    "/": ("index.html", "text/html; charset=utf-8"),
    "/app.js": ("app.js", "text/javascript; charset=utf-8"),
    "/style.css": ("style.css", "text/css; charset=utf-8"),
    "/i18n.json": ("i18n/zh-CN.json", "application/json; charset=utf-8"),
    "/planning": ("planning.html", "text/html; charset=utf-8"),
    "/catalog.js": ("catalog.js", "text/javascript; charset=utf-8"),
}


class PreviewReader:
    def __init__(self, state):
        self.root = Path(state).resolve()
        self.database = self.root / "wiki-preview.sqlite3"

    def connect(self):
        db = sqlite3.connect(self.database.as_uri() + "?mode=ro", uri=True)
        db.row_factory = sqlite3.Row
        db.execute("PRAGMA query_only=ON")
        return db

    def snapshots(self):
        if not self.database.exists():
            return []
        with closing(self.connect()) as db:
            return [
                dict(row)
                for row in db.execute(
                    "SELECT s.id,s.repo_id,s.branch,s.commit_sha,s.created_at,"
                    "EXISTS(SELECT 1 FROM current_previews c WHERE c.snapshot_id=s.id) "
                    "AS is_current FROM snapshots s ORDER BY s.created_at DESC,s.rowid DESC"
                )
            ]

    def snapshot(self, identity):
        if not re.fullmatch(r"[a-f0-9]{64}", identity):
            raise ValueError("Invalid snapshot identity")
        with closing(self.connect()) as db:
            row = db.execute(
                "SELECT s.artifact,s.artifact_hash,j.inputs FROM snapshots s "
                "JOIN jobs j ON j.id=s.job_id WHERE s.id=?",
                (identity,),
            ).fetchone()
        if row is None:
            raise ValueError("Unknown snapshot")
        path = (self.root / row["artifact"]).resolve()
        if not path.is_relative_to(self.root / "snapshots"):
            raise ValueError("Snapshot artifact is outside the snapshot directory")
        body = path.read_bytes()
        if hashlib.sha256(body).hexdigest() != row["artifact_hash"]:
            raise ValueError("Snapshot integrity check failed")
        payload = json.loads(body)
        if (
            payload.get("mode") != "preview_only"
            or payload.get("generation_enabled") is not False
            or payload.get("model_calls") != 0
            or payload.get("schema") not in {1, 2, 3}
        ):
            raise ValueError("Expected a supported generation-free preview")
        inputs = json.loads(row["inputs"])
        return {
            "snapshot_id": identity,
            "preview": payload,
            "catalog": inputs.get("catalog", []),
            "scope": inputs.get("scope", []),
            "repository": inputs.get("repo_path", payload["repo_id"]),
        }


def make_server(state, port=5488):
    reader = PreviewReader(state)

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, format, *args):
            pass

        def send_body(self, status, body, content_type):
            self.send_response(status)
            self.send_header("Content-Type", content_type)
            self.send_header("Content-Length", str(len(body)))
            self.send_header("Cache-Control", "no-store")
            self.send_header("X-Content-Type-Options", "nosniff")
            self.send_header("Referrer-Policy", "no-referrer")
            self.send_header(
                "Content-Security-Policy",
                "default-src 'self'; "
                "script-src 'self'; style-src 'self'; "
                "connect-src 'self'; frame-ancestors 'none'; base-uri 'none'",
            )
            self.end_headers()
            self.wfile.write(body)

        def do_GET(self):
            path = urlsplit(self.path).path
            try:
                if path in ASSETS:
                    name, content_type = ASSETS[path]
                    self.send_body(200, (HERE / "ui" / name).read_bytes(), content_type)
                    return
                if path == "/api/snapshots":
                    result = reader.snapshots()
                elif path.startswith("/api/snapshots/"):
                    result = reader.snapshot(path.removeprefix("/api/snapshots/"))
                else:
                    self.send_body(404, b'{"error":"Not found"}', "application/json")
                    return
                self.send_body(
                    200,
                    json.dumps(result, ensure_ascii=True).encode(),
                    "application/json; charset=utf-8",
                )
            except (OSError, ValueError, KeyError, sqlite3.Error):
                self.send_body(
                    422,
                    b'{"error":"Preview could not be read or verified"}',
                    "application/json",
                )

    return ThreadingHTTPServer(("127.0.0.1", port), Handler)


def main():
    parser = argparse.ArgumentParser(description="Read-only local Wiki impact viewer")
    parser.add_argument(
        "--state", type=Path, default=HERE.parents[1] / ".ocr-data/wiki-preview"
    )
    parser.add_argument("--port", type=int, default=5488)
    args = parser.parse_args()
    with make_server(args.state, args.port) as server:
        print(f"Local: http://127.0.0.1:{server.server_port}/", flush=True)
        try:
            server.serve_forever()
        except KeyboardInterrupt:
            pass


if __name__ == "__main__":
    main()
