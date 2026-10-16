ALTER TABLE review_executions ADD COLUMN phase TEXT NOT NULL DEFAULT '';
ALTER TABLE review_executions ADD COLUMN stop_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE review_tool_calls ADD COLUMN question_id TEXT NOT NULL DEFAULT '';
CREATE TABLE review_questions (
    job_id INTEGER NOT NULL REFERENCES review_jobs(id),
    question_id TEXT NOT NULL,
    question TEXT NOT NULL,
    state TEXT NOT NULL CHECK(state IN ('open','verified','unresolved')),
    conclusion TEXT NOT NULL DEFAULT '',
    next_lookup TEXT NOT NULL DEFAULT '',
    evidence_json TEXT NOT NULL DEFAULT '[]',
    needs_update INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(job_id,question_id)
);
