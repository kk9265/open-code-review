// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package platform

import (
	"context"
	"encoding/json"
)

type ReviewQuestion struct {
	ID          string  `json:"id"`
	Question    string  `json:"question"`
	State       string  `json:"state"`
	Conclusion  string  `json:"conclusion"`
	NextLookup  string  `json:"next_lookup"`
	Evidence    []int64 `json:"evidence_ids"`
	NeedsUpdate bool    `json:"needs_update"`
}

func (s *Server) reviewQuestions(ctx context.Context, job int64) ([]ReviewQuestion, error) {
	rows, err := s.store.db.QueryContext(ctx, `SELECT question_id,question,state,conclusion,next_lookup,evidence_json,needs_update FROM review_questions WHERE job_id=? ORDER BY rowid`, job)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var questions []ReviewQuestion
	for rows.Next() {
		var q ReviewQuestion
		var evidence string
		if err = rows.Scan(&q.ID, &q.Question, &q.State, &q.Conclusion, &q.NextLookup, &evidence, &q.NeedsUpdate); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(evidence), &q.Evidence); err != nil {
			return nil, err
		}
		questions = append(questions, q)
	}
	return questions, rows.Err()
}
