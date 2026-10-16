// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var ticketPattern = regexp.MustCompile(`https://project\.feishu\.cn/([A-Za-z0-9_-]+)/(story|issue)/detail/([0-9]+)\b`)

type TicketCommit struct {
	ID, RepositoryID, GroupID, Version                            int64
	SHA, Branch, Message, Parents, Reason, RepositoryName, Ticket string
}

type ReviewJob struct {
	RoundNumber                                                              int
	BudgetEnabled                                                            bool
	Execution                                                                *ReviewExecution
	FeedbackDraft, Feedback                                                  string
	FeedbackVersion, ContextJobID                                            int64
	RenderedResult                                                           template.HTML
	Title                                                                    string
	ModelSource                                                              string
	ID, GroupID, RepositoryID                                                int64
	State, Ticket, Branch, RepositoryName, CommitsJSON, Input, Result, Error string
	Endpoint, Model, Language, CreatedAt, StartedAt, FinishedAt              string
	Budget                                                                   int
	Frozen                                                                   bool
	Commits                                                                  []TicketCommit
}

func commitTitle(message string) string {
	for _, line := range strings.Split(message, "\n") {
		line = strings.TrimSpace(ticketPattern.ReplaceAllString(line, ""))
		line = strings.TrimSpace(strings.TrimPrefix(line, "[t]"))
		if line != "" {
			return line
		}
	}
	return "Untitled ticket"
}

func (c TicketCommit) Title() string { return commitTitle(c.Message) }

func ticketLinks(message string) []string {
	seen := map[string]bool{}
	var result []string
	for _, link := range ticketPattern.FindAllString(message, -1) {
		if !seen[link] {
			seen[link] = true
			result = append(result, link)
		}
	}
	return result
}

func queueGroup(ctx context.Context, tx *sql.Tx, group int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO review_jobs(group_id,state,created_at) SELECT ?,'queued',? WHERE NOT EXISTS (SELECT 1 FROM review_jobs WHERE group_id=? AND state='queued' AND frozen=0)`, group, now(), group)
	return err
}

func findTicketGroup(ctx context.Context, tx *sql.Tx, repository int64, branch, ticket string) (int64, error) {
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO ticket_groups(repository_id,branch,ticket,created_at) VALUES (?,?,?,?)`, repository, branch, ticket, now())
	if err != nil {
		return 0, err
	}
	var id int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM ticket_groups WHERE repository_id=? AND branch=? AND ticket=?`, repository, branch, ticket).Scan(&id)
	return id, err
}

// Discovery is retried independently of synchronization, so a failed classification cannot lose commits.
func (s *Server) collectTickets(ctx context.Context) error {
	rows, err := s.store.db.QueryContext(ctx, `SELECT t.repository_id,t.sha,r.branch FROM tracked_commits t JOIN repository_tracking r ON r.repository_id=t.repository_id JOIN repositories p ON p.id=t.repository_id WHERE p.archived=0 AND NOT EXISTS (SELECT 1 FROM ticket_commits c WHERE c.repository_id=t.repository_id AND c.sha=t.sha) ORDER BY t.rowid LIMIT 200`)
	if err != nil {
		return err
	}
	var pending []TicketCommit
	for rows.Next() {
		var c TicketCommit
		if err = rows.Scan(&c.RepositoryID, &c.SHA, &c.Branch); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, c := range pending {
		path, err := s.cachePath(c.RepositoryID)
		if err != nil {
			return err
		}
		metadata, err := trackingGit(ctx, path, "show", "-s", "--format=%P%x00%B", c.SHA, "--")
		if err != nil {
			return err
		}
		c.Parents, c.Message, _ = strings.Cut(metadata, "\x00")
		// Keep every admitted commit reachable even after a later force push.
		if err = runManagedGit(ctx, nil, "--git-dir="+path, "update-ref", "refs/ocr/commits/"+c.SHA, c.SHA); err != nil {
			return err
		}
		links := ticketLinks(c.Message)
		switch {
		case len(strings.Fields(c.Parents)) > 1:
			c.Reason = "Merge commit: assign explicitly to review its first-parent patch."
		case len(links) == 0:
			c.Reason = "No ticket link found."
		case len(links) > 1:
			c.Reason = "Multiple ticket links found."
		}
		tx, err := s.store.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		var group any
		if c.Reason == "" {
			c.GroupID, err = findTicketGroup(ctx, tx, c.RepositoryID, c.Branch, links[0])
			group = c.GroupID
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO ticket_commits(repository_id,sha,branch,message,parents,reason,group_id) VALUES (?,?,?,?,?,?,?)`, c.RepositoryID, c.SHA, c.Branch, c.Message, c.Parents, c.Reason, group)
		}
		if err == nil && c.GroupID != 0 {
			err = queueGroup(ctx, tx, c.GroupID)
		}
		if err != nil {
			tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

const jobColumns = `j.id,j.group_id,g.repository_id,j.state,g.ticket,g.branch,p.name,j.frozen,j.commits_json,j.input,j.result,j.error,j.endpoint,j.model,j.language,j.budget,j.created_at,j.started_at,j.finished_at,j.model_source,
COALESCE(CASE WHEN json_valid(j.commits_json) THEN json_extract(j.commits_json,'$[0].Message') END,(SELECT message FROM ticket_commits WHERE group_id=j.group_id ORDER BY id LIMIT 1),''),j.feedback_draft,j.feedback_version,j.feedback,COALESCE(j.context_job_id,0),j.budget_enabled`
const jobJoin = ` FROM review_jobs j JOIN ticket_groups g ON g.id=j.group_id JOIN repositories p ON p.id=g.repository_id `

func scanJob(row interface{ Scan(...any) error }) (ReviewJob, error) {
	var j ReviewJob
	err := row.Scan(&j.ID, &j.GroupID, &j.RepositoryID, &j.State, &j.Ticket, &j.Branch, &j.RepositoryName, &j.Frozen, &j.CommitsJSON, &j.Input, &j.Result, &j.Error, &j.Endpoint, &j.Model, &j.Language, &j.Budget, &j.CreatedAt, &j.StartedAt, &j.FinishedAt, &j.ModelSource, &j.Title, &j.FeedbackDraft, &j.FeedbackVersion, &j.Feedback, &j.ContextJobID, &j.BudgetEnabled)
	j.Title = commitTitle(j.Title)
	if err == nil && j.CommitsJSON != "" {
		err = json.Unmarshal([]byte(j.CommitsJSON), &j.Commits)
	}
	return j, err
}

func (s *Server) queuePage(c *gin.Context) {
	ctx := c.Request.Context()
	data := page{Page: "queue", Title: s.text("Review queue")}
	if err := s.store.db.QueryRowContext(ctx, "SELECT enabled FROM review_queue WHERE id=1").Scan(&data.QueueEnabled); err != nil {
		s.fail(c, err)
		return
	}
	settings, err := s.store.Settings(ctx)
	if err != nil {
		s.fail(c, err)
		return
	}
	data.QueueStatus = s.reviewReadiness(settings)
	rows, err := s.store.db.QueryContext(ctx, `SELECT `+jobListColumns()+jobJoin+` WHERE j.state IN ('queued','running') ORDER BY j.id LIMIT 100`)
	if err != nil {
		s.fail(c, err)
		return
	}
	for rows.Next() {
		j, e := scanJob(rows)
		if e != nil {
			rows.Close()
			s.fail(c, e)
			return
		}
		data.Jobs = append(data.Jobs, j)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		s.fail(c, err)
		return
	}
	rows, err = s.store.db.QueryContext(ctx, `SELECT c.id,c.repository_id,COALESCE(c.group_id,0),c.version,c.sha,c.branch,c.message,c.parents,c.reason,p.name,COALESCE(g.ticket,'') FROM ticket_commits c JOIN repositories p ON p.id=c.repository_id LEFT JOIN ticket_groups g ON g.id=c.group_id ORDER BY c.group_id IS NULL DESC,c.id DESC LIMIT 100`)
	if err != nil {
		s.fail(c, err)
		return
	}
	for rows.Next() {
		var v TicketCommit
		err = rows.Scan(&v.ID, &v.RepositoryID, &v.GroupID, &v.Version, &v.SHA, &v.Branch, &v.Message, &v.Parents, &v.Reason, &v.RepositoryName, &v.Ticket)
		if err != nil {
			rows.Close()
			s.fail(c, err)
			return
		}
		data.TicketCommits = append(data.TicketCommits, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		s.fail(c, err)
		return
	}
	s.render(c, http.StatusOK, data)
}

func (s *Server) jobPage(c *gin.Context) {
	id, err := recordID(c)
	if err != nil {
		s.fail(c, err)
		return
	}
	j, err := s.resultJob(c.Request.Context(), id)
	if err != nil {
		s.fail(c, err)
		return
	}

	data := page{Page: "job", Title: s.text(j.Title), Job: j}
	if j.State != "queued" && j.State != "running" {
		data.Page = "results"
		if err := s.resultTree(c, &data, 0); err != nil {
			s.fail(c, err)
			return
		}
	}
	s.render(c, http.StatusOK, data)
}

func (s *Server) queueControl(c *gin.Context) {
	enabled := c.PostForm("enabled") == "1"
	if enabled {
		v, err := s.store.Settings(c.Request.Context())
		if err != nil {
			s.fail(c, err)
			return
		}
		if s.reviewReadiness(v) != "" {
			c.Redirect(http.StatusSeeOther, "/reviews/queue")
			return
		}
	}
	_, err := s.store.db.ExecContext(c.Request.Context(), "UPDATE review_queue SET enabled=? WHERE id=1", enabled)
	if err != nil {
		s.fail(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/reviews/queue?saved=1")
}

func (s *Server) assignTicket(c *gin.Context) {
	id, err := recordID(c)
	if err != nil {
		s.fail(c, err)
		return
	}
	ticket := strings.TrimSpace(c.PostForm("ticket"))
	links := ticketLinks(ticket)
	if len(links) != 1 || links[0] != ticket {
		s.render(c, http.StatusUnprocessableEntity, page{Page: "error", Title: s.text("Invalid form"), Error: "Enter one complete Feishu story or issue URL."})
		return
	}
	ctx := c.Request.Context()
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		s.fail(c, err)
		return
	}
	defer tx.Rollback()
	var repository, old, ver int64
	var branch string
	err = tx.QueryRowContext(ctx, `SELECT c.repository_id,c.branch,COALESCE(c.group_id,0),c.version FROM ticket_commits c JOIN repositories r ON r.id=c.repository_id WHERE c.id=? AND r.archived=0`, id).Scan(&repository, &branch, &old, &ver)
	if err != nil {
		s.fail(c, err)
		return
	}
	if ver != version(c) {
		s.fail(c, ErrConflict)
		return
	}
	group, err := findTicketGroup(ctx, tx, repository, branch, ticket)
	if err != nil {
		s.fail(c, err)
		return
	}
	if old != group {
		_, err = tx.ExecContext(ctx, `UPDATE ticket_commits SET group_id=?,reason='',version=version+1 WHERE id=?`, group, id)
		if err == nil {
			err = queueGroup(ctx, tx, group)
		}
		if err == nil && old != 0 {
			var count int
			err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM ticket_commits WHERE group_id=?`, old).Scan(&count)
			if err == nil && count > 0 {
				err = queueGroup(ctx, tx, old)
			}
			if err == nil && count == 0 {
				_, err = tx.ExecContext(ctx, `UPDATE review_jobs SET state='cancelled',finished_at=? WHERE group_id=? AND state='queued' AND frozen=0`, now(), old)
			}
		}
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.fail(c, err)
		return
	}
	if c.PostForm("return_to") == "unclassified" {
		c.Redirect(http.StatusSeeOther, "/reviews?unclassified=1&saved=1")
		return
	}
	c.Redirect(http.StatusSeeOther, "/reviews/queue?saved=1")
}

func (s *Server) jobAction(c *gin.Context) {
	id, err := recordID(c)
	if err != nil {
		s.fail(c, err)
		return
	}
	ctx := c.Request.Context()
	if c.PostForm("job_action") == "feedback" || c.PostForm("job_action") == "rerun" {
		s.reviewFeedback(c, id)
		return
	}
	if c.PostForm("job_action") == "retry" {
		// A retry is a separate run with exactly the same frozen input; the failed record remains intact.
		result, e := s.store.db.ExecContext(ctx, `INSERT INTO review_jobs(group_id,state,frozen,commits_json,input,endpoint,model,language,budget,retry_of,created_at,model_source,feedback,context_job_id,budget_enabled)
	   SELECT group_id,'queued',1,commits_json,input,endpoint,model,language,budget,id,?,model_source,feedback,context_job_id,budget_enabled FROM review_jobs WHERE id=? AND state IN ('failed','interrupted') AND NOT EXISTS(SELECT 1 FROM review_jobs WHERE retry_of=? AND state IN ('queued','running'))`, now(), id, id)
		err = changed(result, e)
	} else if c.PostForm("job_action") == "cancel" {
		result, e := s.store.db.ExecContext(ctx, `UPDATE review_jobs SET state='cancelled',finished_at=? WHERE id=? AND state='queued'`, now(), id)
		err = changed(result, e)
	} else {
		err = ErrConflict
	}
	if err != nil {
		s.fail(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, "/reviews/queue?saved=1")
}

func (s *Server) claimReview(ctx context.Context, settings Settings) (*ReviewJob, error) {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	j, err := scanJob(tx.QueryRowContext(ctx, `SELECT `+jobColumns+jobJoin+`WHERE j.state='queued' AND p.archived=0 AND (SELECT enabled FROM review_queue WHERE id=1)=1 ORDER BY j.id LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !j.Frozen {
		rows, err := tx.QueryContext(ctx, `SELECT id,sha,message,parents FROM ticket_commits WHERE group_id=? ORDER BY id`, j.GroupID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var c TicketCommit
			if err = rows.Scan(&c.ID, &c.SHA, &c.Message, &c.Parents); err != nil {
				rows.Close()
				return nil, err
			}
			j.Commits = append(j.Commits, c)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		b, err := json.Marshal(j.Commits)
		if err != nil {
			return nil, err
		}
		j.CommitsJSON = string(b)
		j.Endpoint, j.Model, j.Language, j.Budget = settings.ModelEndpoint, settings.ModelName, settings.Language, settings.TokenBudget
		j.ModelSource = settings.ModelSource
		j.BudgetEnabled = settings.TokenBudgetEnabled
	}
	_, err = tx.ExecContext(ctx, `UPDATE review_jobs SET state='running',frozen=1,commits_json=?,endpoint=?,model=?,language=?,budget=?,started_at=?,model_source=?,budget_enabled=? WHERE id=?`, j.CommitsJSON, j.Endpoint, j.Model, j.Language, j.Budget, now(), j.ModelSource, j.BudgetEnabled, j.ID)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &j, nil
}

func (s *Server) RunReviewQueue(ctx context.Context) {
	if _, err := s.store.db.ExecContext(ctx, `UPDATE review_jobs SET state='interrupted',error='The workbench stopped during this run. Retry manually.',finished_at=? WHERE state='running'`, now()); err != nil {
		s.log.Error("recover review queue", "error", err)
		return
	}
	for ctx.Err() == nil {
		discoveryCtx, cancel := context.WithTimeout(ctx, time.Minute)
		err := s.collectTickets(discoveryCtx)
		cancel()
		if err != nil && ctx.Err() == nil {
			s.log.Error("classify commits", "error", err)
		}
		settings, err := s.store.Settings(ctx)
		if err == nil {
			settings, err = resolveReviewSettings(settings)
		}
		if err == nil && s.reviewReadiness(settings) == "" {
			j, err := s.claimReview(ctx, settings)
			if err != nil {
				s.log.Error("claim review", "error", err)
			}
			if j != nil {
				runCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
				result, runErr := s.executeQueuedReview(runCtx, j)
				cancel()
				state, message := "completed", ""
				if runErr != nil {
					state, message = "failed", runErr.Error()
				}
				if ctx.Err() != nil {
					state, message = "interrupted", "The workbench stopped during this run. Retry manually."
				}
				saveCtx, saveCancel := context.WithTimeout(context.Background(), 5*time.Second)
				_, err = s.store.db.ExecContext(saveCtx, `UPDATE review_jobs SET state=?,result=?,error=?,finished_at=? WHERE id=? AND state='running'`, state, result, message, now(), j.ID)
				saveCancel()
				if err != nil {
					s.log.Error("save review result", "job_id", j.ID, "error", err)
					return
				}
				continue
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}
