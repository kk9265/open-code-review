CREATE TABLE gitea_repositories (
    repository_id INTEGER PRIMARY KEY REFERENCES repositories(id),
    server_url TEXT NOT NULL,
    remote_id INTEGER NOT NULL,
    full_name TEXT NOT NULL,
    default_branch TEXT NOT NULL DEFAULT '',
    sync_state TEXT NOT NULL DEFAULT 'queued' CHECK (sync_state IN ('queued', 'running', 'ready', 'failed', 'interrupted')),
    sync_error TEXT NOT NULL DEFAULT '',
    synced_at TEXT NOT NULL DEFAULT '',
    UNIQUE(server_url, remote_id)
);
