CREATE TABLE ticket_groups (
    id INTEGER PRIMARY KEY,
    repository_id INTEGER NOT NULL REFERENCES repositories(id),
    branch TEXT NOT NULL,
    ticket TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(repository_id,branch,ticket)
);
CREATE TABLE ticket_commits (
    id INTEGER PRIMARY KEY,
    repository_id INTEGER NOT NULL,
    sha TEXT NOT NULL,
    branch TEXT NOT NULL,
    message TEXT NOT NULL,
    parents TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    group_id INTEGER REFERENCES ticket_groups(id),
    version INTEGER NOT NULL DEFAULT 1,
    FOREIGN KEY(repository_id,sha) REFERENCES tracked_commits(repository_id,sha),
    UNIQUE(repository_id,sha)
);
CREATE TABLE review_jobs (
    id INTEGER PRIMARY KEY,
    group_id INTEGER NOT NULL REFERENCES ticket_groups(id),
    state TEXT NOT NULL CHECK(state IN ('queued','running','completed','failed','interrupted','cancelled')),
    frozen INTEGER NOT NULL DEFAULT 0,
    commits_json TEXT NOT NULL DEFAULT '',
    input TEXT NOT NULL DEFAULT '',
    result TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    endpoint TEXT NOT NULL DEFAULT '',
    model_source TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    language TEXT NOT NULL DEFAULT '',
    budget INTEGER NOT NULL DEFAULT 0,
    retry_of INTEGER REFERENCES review_jobs(id),
    created_at TEXT NOT NULL,
    started_at TEXT NOT NULL DEFAULT '',
    finished_at TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX one_pending_ticket ON review_jobs(group_id) WHERE state='queued' AND frozen=0;
CREATE UNIQUE INDEX one_running_review ON review_jobs(state) WHERE state='running';
CREATE TABLE review_queue (id INTEGER PRIMARY KEY CHECK(id=1), enabled INTEGER NOT NULL DEFAULT 0);
INSERT INTO review_queue(id,enabled) VALUES(1,0);
