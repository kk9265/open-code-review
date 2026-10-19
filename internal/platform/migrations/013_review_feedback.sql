CREATE TABLE finding_assessments (
    job_id INTEGER NOT NULL REFERENCES review_jobs(id),
    finding_key TEXT NOT NULL,
    assessment TEXT NOT NULL CHECK (assessment IN ('accurate','false_positive','uncertain','resolved')),
    PRIMARY KEY (job_id, finding_key)
);
INSERT INTO finding_assessments SELECT job_id, finding_key, 'accurate' FROM finding_likes;
ALTER TABLE finding_comments ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE finding_comments ADD COLUMN updated_at TEXT NOT NULL DEFAULT '';
