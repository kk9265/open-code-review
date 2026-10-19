ALTER TABLE review_jobs ADD COLUMN purpose_input TEXT NOT NULL DEFAULT '';
ALTER TABLE review_jobs ADD COLUMN generated_purpose TEXT NOT NULL DEFAULT '';
ALTER TABLE review_jobs ADD COLUMN purpose_state TEXT NOT NULL DEFAULT '';
ALTER TABLE review_jobs ADD COLUMN purpose_source TEXT NOT NULL DEFAULT '';
ALTER TABLE review_jobs ADD COLUMN purpose_requests INTEGER NOT NULL DEFAULT 0;
ALTER TABLE review_jobs ADD COLUMN purpose_tokens INTEGER NOT NULL DEFAULT 0;
UPDATE review_jobs SET purpose_source='manual' WHERE change_purpose<>'';
