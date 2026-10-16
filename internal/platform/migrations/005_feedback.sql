ALTER TABLE review_jobs ADD COLUMN feedback_draft TEXT NOT NULL DEFAULT '';
ALTER TABLE review_jobs ADD COLUMN feedback_version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE review_jobs ADD COLUMN feedback TEXT NOT NULL DEFAULT '';
ALTER TABLE review_jobs ADD COLUMN context_job_id INTEGER REFERENCES review_jobs(id);
