ALTER TABLE review_jobs ADD COLUMN budget_enabled INTEGER NOT NULL DEFAULT 0 CHECK (budget_enabled IN (0,1));
