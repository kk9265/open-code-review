CREATE TABLE settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    payload TEXT NOT NULL CHECK (json_valid(payload)),
    version INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE repositories (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    remote_url TEXT NOT NULL DEFAULT '',
    local_path TEXT NOT NULL DEFAULT '',
    archived INTEGER NOT NULL DEFAULT 0 CHECK (archived IN (0, 1)),
    version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX repositories_remote ON repositories(remote_url) WHERE remote_url <> '';
CREATE UNIQUE INDEX repositories_local ON repositories(local_path) WHERE local_path <> '';

CREATE TABLE reviews (
    id INTEGER PRIMARY KEY,
    repository_id INTEGER NOT NULL REFERENCES repositories(id),
    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    background TEXT NOT NULL DEFAULT '',
    base_ref TEXT NOT NULL DEFAULT '',
    head_ref TEXT NOT NULL DEFAULT '',
    archived INTEGER NOT NULL DEFAULT 0 CHECK (archived IN (0, 1)),
    version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX reviews_repository ON reviews(repository_id);
CREATE INDEX reviews_recent ON reviews(archived, updated_at DESC);
