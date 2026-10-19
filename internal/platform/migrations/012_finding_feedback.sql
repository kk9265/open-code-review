CREATE TABLE finding_likes (
    job_id INTEGER NOT NULL REFERENCES review_jobs(id),
    finding_key TEXT NOT NULL,
    PRIMARY KEY (job_id, finding_key)
);
CREATE TABLE finding_comments (
    id INTEGER PRIMARY KEY,
    job_id INTEGER NOT NULL REFERENCES review_jobs(id),
    finding_key TEXT NOT NULL,
    content TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX finding_comments_run ON finding_comments(job_id, finding_key, id);
