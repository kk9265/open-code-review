CREATE TABLE review_executions (
    job_id INTEGER PRIMARY KEY REFERENCES review_jobs(id),
    context_sha TEXT NOT NULL,
    engine TEXT NOT NULL,
    requests INTEGER NOT NULL DEFAULT 0,
    tokens INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE review_tool_calls (
    id INTEGER PRIMARY KEY,
    job_id INTEGER NOT NULL REFERENCES review_jobs(id),
    name TEXT NOT NULL,
    revision TEXT NOT NULL,
    arguments TEXT NOT NULL,
    result TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL DEFAULT 'running',
    created_at TEXT NOT NULL
);
CREATE INDEX review_tool_calls_job ON review_tool_calls(job_id, id);
