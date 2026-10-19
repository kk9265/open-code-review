// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package platform

import (
	"context"
	"strings"
)

func (s *Server) compareReviewFindings(ctx context.Context, job *ReviewJob) error {
	if job.State != "completed" || job.Execution == nil || (job.Execution.Direct == nil && len(job.Execution.Native) == 0) {
		return nil
	}
	var previousID int64
	err := s.store.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT id FROM review_jobs WHERE group_id=? AND id<? AND state='completed' ORDER BY CASE WHEN id=? THEN 0 ELSE 1 END,id DESC LIMIT 1),0)`, job.GroupID, job.ID, job.ContextJobID).Scan(&previousID)
	if err != nil || previousID == 0 {
		return err
	}
	previous, err := s.resultJobDetails(ctx, previousID)
	if err != nil || previous.Execution == nil || (previous.Execution.Direct == nil && len(previous.Execution.Native) == 0) {
		return err
	}
	job.ComparisonJobID = previousID
	job.PreviousFindings = compareFindingCards(job.FindingCards, previous.FindingCards)
	return nil
}

func compareFindingCards(current, previous []FindingCard) []FindingCard {
	titleKey := func(card FindingCard) string {
		return card.Path + "\x00" + strings.Join(strings.Fields(strings.ToLower(card.Title)), " ")
	}
	currentTitles, previousTitles := map[string]int{}, map[string]int{}
	for _, card := range current {
		currentTitles[titleKey(card)]++
	}
	for _, card := range previous {
		previousTitles[titleKey(card)]++
	}
	matched := make([]bool, len(previous))
	for i := range current {
		current[i].Change = "new"
		for j, old := range previous {
			if !matched[j] && current[i].Key == old.Key {
				matched[j], current[i].Change = true, "persisting"
				break
			}
		}
	}
	for i := range current {
		if current[i].Change != "new" {
			continue
		}
		key := titleKey(current[i])
		if currentTitles[key] != 1 || previousTitles[key] != 1 || strings.TrimSpace(current[i].Title) == "" {
			continue
		}
		for j, old := range previous {
			if !matched[j] && titleKey(old) == key {
				matched[j], current[i].Change = true, "persisting"
				break
			}
		}
	}
	var missing []FindingCard
	for i, old := range previous {
		if matched[i] {
			continue
		}
		old.Change = "not_reported"
		if old.Assessment == "resolved" {
			old.Change = "resolved"
		}
		missing = append(missing, old)
	}
	return missing
}
