CREATE TABLE repository_tracking (
    repository_id INTEGER PRIMARY KEY REFERENCES repositories(id),
    branch TEXT NOT NULL,
    sync_mode TEXT NOT NULL CHECK (sync_mode IN ('http','local')),
    local_source TEXT NOT NULL DEFAULT '',
    interval_seconds INTEGER NOT NULL CHECK (interval_seconds BETWEEN 60 AND 3600),
    state TEXT NOT NULL CHECK (state IN ('pending','active','paused','blocked')),
    baseline_sha TEXT NOT NULL DEFAULT '',
    last_sha TEXT NOT NULL DEFAULT '',
    last_checked TEXT NOT NULL DEFAULT '',
    next_check INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    version INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE tracked_commits (
    repository_id INTEGER NOT NULL REFERENCES repository_tracking(repository_id),
    sha TEXT NOT NULL,
    discovered_at TEXT NOT NULL,
    PRIMARY KEY(repository_id,sha)
);
