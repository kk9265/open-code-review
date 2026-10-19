// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package platform

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

func (s *Server) reviewFeedback(c *gin.Context, id int64) {
	ctx := c.Request.Context()
	feedback := strings.TrimSpace(c.PostForm("feedback"))
	purpose, hasPurpose := c.GetPostForm("change_purpose")
	purpose = strings.TrimSpace(purpose)
	if utf8.RuneCountInString(purpose) > 10000 {
		s.render(c, http.StatusUnprocessableEntity, page{Page: "error", Title: s.text("Invalid form"), Error: "Change purpose must be at most 10000 characters."})
		return
	}
	if utf8.RuneCountInString(feedback) > 10000 {
		s.render(c, http.StatusUnprocessableEntity, page{Page: "error", Title: s.text("Invalid form"), Error: "Feedback must be at most 10000 characters."})
		return
	}
	version, _ := strconv.ParseInt(c.PostForm("feedback_version"), 10, 64)
	findingFeedback := ""
	if c.PostForm("job_action") == "rerun" {
		var err error
		findingFeedback, err = s.findingFeedbackText(ctx, id)
		if err != nil {
			s.fail(c, err)
			return
		}
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		s.fail(c, err)
		return
	}
	defer tx.Rollback()
	if hasPurpose {
		var existing, generated string
		if err := tx.QueryRowContext(ctx, `SELECT g.change_purpose,j.generated_purpose FROM review_jobs j JOIN ticket_groups g ON g.id=j.group_id WHERE j.id=?`, id).Scan(&existing, &generated); err != nil {
			s.fail(c, err)
			return
		}
		// Saving an unchanged generated draft must not pin it as a manual override for future commits.
		if existing == "" && purpose == generated {
			purpose = ""
		}
		purposeVersion, _ := strconv.ParseInt(c.PostForm("purpose_version"), 10, 64)
		result, err := tx.ExecContext(ctx, `UPDATE ticket_groups SET change_purpose=?,purpose_version=purpose_version+1 WHERE id=(SELECT group_id FROM review_jobs WHERE id=?) AND purpose_version=?`, purpose, id, purposeVersion)
		if err = changed(result, err); err != nil {
			s.fail(c, err)
			return
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE review_jobs SET feedback_draft=?,feedback_version=feedback_version+1 WHERE id=? AND feedback_version=?`, feedback, id, version)
	if err = changed(result, err); err != nil {
		s.fail(c, err)
		return
	}
	target := id
	if c.PostForm("job_action") == "rerun" {
		var settings Settings
		var payload string
		err = tx.QueryRowContext(ctx, "SELECT payload FROM settings WHERE id=1").Scan(&payload)
		if err == nil {
			err = json.Unmarshal([]byte(payload), &settings)
		}
		if err == nil {
			settings, err = resolveReviewSettings(settings)
		}
		if err != nil {
			s.fail(c, err)
			return
		}
		result, err = tx.ExecContext(ctx, `INSERT INTO review_jobs(group_id,state,frozen,commits_json,endpoint,model,language,budget,created_at,model_source,feedback,context_job_id,budget_enabled,change_purpose)
   SELECT j.group_id,'queued',1,j.commits_json,?,?,?,?,?,?,?,j.id,?,g.change_purpose FROM review_jobs j JOIN ticket_groups g ON g.id=j.group_id JOIN repositories r ON r.id=g.repository_id WHERE j.id=? AND j.frozen=1 AND j.state NOT IN ('queued','running') AND r.archived=0`, settings.ModelEndpoint, settings.ModelName, settings.Language, settings.TokenBudget, now(), settings.ModelSource, feedback+findingFeedback, settings.TokenBudgetEnabled, id)
		if err = changed(result, err); err != nil {
			s.fail(c, err)
			return
		}
		target, err = result.LastInsertId()
		if err != nil {
			s.fail(c, err)
			return
		}
	}
	if err = tx.Commit(); err != nil {
		s.fail(c, err)
		return
	}
	path := "/reviews/runs/%d?saved=1"
	if c.PostForm("view") == "process" {
		path = "/reviews/queue/runs/%d?saved=1"
	}
	c.Redirect(http.StatusSeeOther, fmt.Sprintf(path, target))
}
