# SPDX-License-Identifier: Apache-2.0
import json
import sqlite3
import time
import uuid
from contextlib import closing, contextmanager
from pathlib import Path

from git_data import digest

SCHEMA = """
CREATE TABLE IF NOT EXISTS jobs (
 id INTEGER PRIMARY KEY AUTOINCREMENT, fingerprint TEXT NOT NULL,
 repo_id TEXT NOT NULL, branch TEXT NOT NULL, inputs TEXT NOT NULL,
 status TEXT NOT NULL, lease_token TEXT, lease_until REAL,
 snapshot_id TEXT, error TEXT, created_at REAL NOT NULL
);
CREATE INDEX IF NOT EXISTS jobs_lookup ON jobs(fingerprint,status);
CREATE TABLE IF NOT EXISTS snapshots (
 id TEXT PRIMARY KEY, job_id INTEGER NOT NULL, repo_id TEXT NOT NULL,
 branch TEXT NOT NULL, commit_sha TEXT NOT NULL, config_fingerprint TEXT NOT NULL,
 artifact TEXT NOT NULL, artifact_hash TEXT NOT NULL, created_at REAL NOT NULL
);
CREATE TABLE IF NOT EXISTS current_previews (
 repo_id TEXT NOT NULL, branch TEXT NOT NULL, snapshot_id TEXT NOT NULL,
 job_id INTEGER NOT NULL, PRIMARY KEY(repo_id,branch)
);
"""


class Store:
    def __init__(self, root):
        self.root = Path(root).resolve()
        self.root.mkdir(parents=True, exist_ok=True)
        self.database = self.root / "wiki-preview.sqlite3"
        with closing(self.connect()) as connection:
            connection.executescript(SCHEMA)

    def connect(self):
        connection = sqlite3.connect(self.database, timeout=30)
        connection.row_factory = sqlite3.Row
        return connection

    @contextmanager
    def transaction(self):
        connection = self.connect()
        try:
            connection.execute("BEGIN IMMEDIATE")
            yield connection
            connection.commit()
        except BaseException:
            connection.rollback()
            raise
        finally:
            connection.close()

    def enqueue(self, inputs):
        key = digest(inputs)
        with self.transaction() as db:
            existing = db.execute(
                "SELECT * FROM jobs WHERE fingerprint=? AND status IN ('queued','running','succeeded') ORDER BY id DESC LIMIT 1",
                (key,),
            ).fetchone()
            if existing:
                return dict(existing), True
            db.execute(
                "UPDATE jobs SET status='superseded' WHERE repo_id=? AND branch=? AND status='queued'",
                (inputs["repo_id"], inputs["branch"]),
            )
            cursor = db.execute(
                "INSERT INTO jobs(fingerprint,repo_id,branch,inputs,status,created_at) VALUES(?,?,?,?,'queued',?)",
                (
                    key,
                    inputs["repo_id"],
                    inputs["branch"],
                    json.dumps(inputs),
                    time.time(),
                ),
            )
            return dict(
                db.execute(
                    "SELECT * FROM jobs WHERE id=?", (cursor.lastrowid,)
                ).fetchone()
            ), False

    def claim(self, lease_seconds=300, job_id=None):
        now = time.time()
        with self.transaction() as db:
            db.execute(
                "UPDATE jobs SET status='queued',lease_token=NULL WHERE status='running' AND lease_until<=?",
                (now,),
            )
            # An expired worker must not revive a request superseded by a later enqueue.
            db.execute(
                "UPDATE jobs SET status='superseded' WHERE status='queued' AND EXISTS (SELECT 1 FROM jobs newer WHERE newer.repo_id=jobs.repo_id AND newer.branch=jobs.branch AND newer.id>jobs.id AND newer.status!='superseded')"
            )
            row = db.execute(
                "SELECT * FROM jobs WHERE status='queued' AND (? IS NULL OR id=?) ORDER BY id LIMIT 1",
                (job_id, job_id),
            ).fetchone()
            if row is None:
                return None
            token = uuid.uuid4().hex
            db.execute(
                "UPDATE jobs SET status='running',lease_token=?,lease_until=?,error=NULL WHERE id=?",
                (token, now + lease_seconds, row["id"]),
            )
            return dict(
                db.execute("SELECT * FROM jobs WHERE id=?", (row["id"],)).fetchone()
            )

    def fail(self, job, error):
        with self.transaction() as db:
            db.execute(
                "UPDATE jobs SET status='failed',error=?,lease_token=NULL WHERE id=? AND status='running' AND lease_token=?",
                (str(error), job["id"], job["lease_token"]),
            )

    def finish(self, job, payload):
        inputs = json.loads(job["inputs"])
        snapshot_id = job["fingerprint"]
        # Flat content identities avoid exceeding Windows path limits.
        path = self.root / "snapshots" / snapshot_id[:2] / (snapshot_id + ".json")
        body = json.dumps(payload, ensure_ascii=True, sort_keys=True, indent=2).encode()
        import hashlib

        body_hash = hashlib.sha256(body).hexdigest()
        with self.transaction() as db:
            current = db.execute(
                "SELECT * FROM jobs WHERE id=?", (job["id"],)
            ).fetchone()
            if (
                current["status"] != "running"
                or current["lease_token"] != job["lease_token"]
                or current["lease_until"] <= time.time()
            ):
                raise RuntimeError("Worker lease expired or replaced")
            path.parent.mkdir(parents=True, exist_ok=True)
            temporary = path.with_name(job["lease_token"] + ".tmp")
            try:
                temporary.write_bytes(body)
                if path.exists():
                    if path.read_bytes() != body:
                        raise RuntimeError("Immutable snapshot content mismatch")
                else:
                    temporary.replace(path)
            finally:
                temporary.unlink(missing_ok=True)
            db.execute(
                "INSERT OR IGNORE INTO snapshots VALUES(?,?,?,?,?,?,?,?,?)",
                (
                    snapshot_id,
                    job["id"],
                    inputs["repo_id"],
                    inputs["branch"],
                    inputs["commit"],
                    inputs["config_fingerprint"],
                    str(path.relative_to(self.root)),
                    body_hash,
                    time.time(),
                ),
            )
            db.execute(
                "UPDATE jobs SET status='succeeded',snapshot_id=?,lease_token=NULL WHERE id=?",
                (snapshot_id, job["id"]),
            )
            newer = db.execute(
                "SELECT 1 FROM jobs WHERE repo_id=? AND branch=? AND id>? AND status!='superseded'",
                (inputs["repo_id"], inputs["branch"], job["id"]),
            ).fetchone()
            if newer is None:
                db.execute(
                    "INSERT INTO current_previews VALUES(?,?,?,?) ON CONFLICT(repo_id,branch) DO UPDATE SET snapshot_id=excluded.snapshot_id,job_id=excluded.job_id",
                    (inputs["repo_id"], inputs["branch"], snapshot_id, job["id"]),
                )
        return snapshot_id

    def list(self, table):
        if table not in {"jobs", "snapshots", "current_previews"}:
            raise ValueError("Unknown table")
        with closing(self.connect()) as db:
            return [
                dict(row)
                for row in db.execute("SELECT * FROM " + table + " ORDER BY rowid")
            ]

    def read_snapshot(self, snapshot_id):
        import hashlib

        with closing(self.connect()) as db:
            row = db.execute(
                "SELECT * FROM snapshots WHERE id=?", (snapshot_id,)
            ).fetchone()
        if row is None:
            raise ValueError("Unknown snapshot")
        body = (self.root / row["artifact"]).read_bytes()
        if hashlib.sha256(body).hexdigest() != row["artifact_hash"]:
            raise RuntimeError("Snapshot integrity check failed")
        return json.loads(body)
