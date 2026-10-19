ALTER TABLE ticket_groups ADD COLUMN change_purpose TEXT NOT NULL DEFAULT '';
ALTER TABLE ticket_groups ADD COLUMN purpose_version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE review_jobs ADD COLUMN change_purpose TEXT NOT NULL DEFAULT '';
