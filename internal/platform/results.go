// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package platform

import (
	"bytes"
	"context"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

func renderReviewMarkdown(source string) (template.HTML, error) {
	var output bytes.Buffer
	// Keep the default renderer restrictions: model output cannot introduce raw HTML or unsafe links.
	md := goldmark.New(goldmark.WithExtensions(extension.GFM))
	if err := md.Convert([]byte(source), &output); err != nil {
		return "", err
	}
	return template.HTML(output.String()), nil
}

func jobListColumns() string {
	return strings.NewReplacer("j.input", "''", "j.result", "CASE WHEN length(j.result)>0 THEN 'saved' ELSE '' END").Replace(strings.Replace(jobColumns, "j.commits_json,", "'',", 1))
}

type ResultGroup struct {
	ID                                int64
	Title, Ticket, Repository, Branch string
	CommitCount                       int
	Rounds                            []ResultRound
}

type ResultRound struct {
	ID                  int64
	Number, CommitCount int
	State, CreatedAt    string
	HasReport           bool
}

func (s *Server) resultsPage(c *gin.Context) {
	_, query, offset := listParams(c)
	data := page{Page: "results", Title: s.text("Review results"), Query: query, Unclassified: c.Query("unclassified") == "1"}
	selected, _ := strconv.ParseInt(c.Query("run"), 10, 64)
	if selected != 0 && !data.Unclassified {
		j, err := s.resultJob(c.Request.Context(), selected)
		if err != nil {
			s.fail(c, err)
			return
		}
		data.Job = j
	}
	if err := s.resultTree(c, &data, offset); err != nil {
		s.fail(c, err)
		return
	}
	if data.Job.ID == 0 && !data.Unclassified {
		for _, group := range data.ResultGroups {
			if len(group.Rounds) > 0 {
				j, err := s.resultJob(c.Request.Context(), group.Rounds[0].ID)
				if err != nil {
					s.fail(c, err)
					return
				}
				data.Job = j
				break
			}
		}
	}
	s.render(c, http.StatusOK, data)
}

func (s *Server) resultJob(ctx context.Context, id int64) (ReviewJob, error) {
	j, err := scanJob(s.store.db.QueryRowContext(ctx, `SELECT `+jobColumns+jobJoin+`WHERE j.id=?`, id))
	if err != nil {
		return j, err
	}
	j.RenderedResult, err = renderReviewMarkdown(j.Result)
	if err == nil {
		j.Execution, err = s.reviewExecution(ctx, j.ID)
	}
	if err == nil {
		err = s.store.db.QueryRowContext(ctx, "SELECT count(*) FROM review_jobs WHERE group_id=? AND id<=?", j.GroupID, j.ID).Scan(&j.RoundNumber)
	}
	return j, err
}

func (s *Server) resultTree(c *gin.Context, data *page, offset int) error {
	ctx := c.Request.Context()
	pattern := "%" + data.Query + "%"
	groupOffset := offset
	if data.Unclassified {
		groupOffset = 0
	}
	const groupColumns = `g.id,g.ticket,p.name,g.branch,
 COALESCE((SELECT message FROM ticket_commits WHERE group_id=g.id ORDER BY id LIMIT 1),
 (SELECT json_extract(commits_json,'$[0].Message') FROM review_jobs WHERE group_id=g.id AND json_valid(commits_json) ORDER BY id LIMIT 1),''),
 (SELECT count(*) FROM ticket_commits WHERE group_id=g.id)`
	const groupJoin = ` FROM ticket_groups g JOIN repositories p ON p.id=g.repository_id `
	rows, err := s.store.db.QueryContext(ctx, `SELECT `+groupColumns+groupJoin+` WHERE
 (g.ticket LIKE ? OR p.name LIKE ? OR EXISTS(SELECT 1 FROM ticket_commits WHERE group_id=g.id AND message LIKE ?)
 OR EXISTS(SELECT 1 FROM review_jobs WHERE group_id=g.id AND json_valid(commits_json) AND json_extract(commits_json,'$[0].Message') LIKE ?))
 ORDER BY COALESCE((SELECT max(id) FROM review_jobs WHERE group_id=g.id),0) DESC,g.id DESC LIMIT 51 OFFSET ?`, pattern, pattern, pattern, pattern, groupOffset)
	if err != nil {
		return err
	}
	for rows.Next() {
		var g ResultGroup
		if err = rows.Scan(&g.ID, &g.Ticket, &g.Repository, &g.Branch, &g.Title, &g.CommitCount); err != nil {
			rows.Close()
			return err
		}
		g.Title = commitTitle(g.Title)
		data.ResultGroups = append(data.ResultGroups, g)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !data.Unclassified {
		pagination(c, data, offset, len(data.ResultGroups))
	}
	if len(data.ResultGroups) > 50 {
		data.ResultGroups = data.ResultGroups[:50]
	}
	found := data.Job.GroupID == 0
	for _, g := range data.ResultGroups {
		found = found || g.ID == data.Job.GroupID
	}
	if !found {
		var g ResultGroup
		err = s.store.db.QueryRowContext(ctx, `SELECT `+groupColumns+groupJoin+` WHERE g.id=?`, data.Job.GroupID).Scan(&g.ID, &g.Ticket, &g.Repository, &g.Branch, &g.Title, &g.CommitCount)
		if err != nil {
			return err
		}
		g.Title = commitTitle(g.Title)
		data.ResultGroups = append(data.ResultGroups, g)
	}
	for i := range data.ResultGroups {
		g := &data.ResultGroups[i]
		rows, err = s.store.db.QueryContext(ctx, `SELECT j.id,(SELECT count(*) FROM review_jobs older WHERE older.group_id=j.group_id AND older.id<=j.id),j.state,j.created_at,length(j.result)>0,
  CASE WHEN json_valid(j.commits_json) THEN json_array_length(j.commits_json) ELSE 0 END
  FROM review_jobs j WHERE j.group_id=? AND j.state NOT IN ('queued','running') ORDER BY j.id DESC`, g.ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var r ResultRound
			if err = rows.Scan(&r.ID, &r.Number, &r.State, &r.CreatedAt, &r.HasReport, &r.CommitCount); err != nil {
				rows.Close()
				return err
			}
			g.Rounds = append(g.Rounds, r)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	if err = s.store.db.QueryRowContext(ctx, `SELECT count(*) FROM ticket_commits WHERE group_id IS NULL`).Scan(&data.UnclassifiedCount); err != nil {
		return err
	}
	if !data.Unclassified {
		return nil
	}
	rows, err = s.store.db.QueryContext(ctx, `SELECT c.id,c.repository_id,0,c.version,c.sha,c.branch,c.message,c.parents,c.reason,p.name,'' FROM ticket_commits c JOIN repositories p ON p.id=c.repository_id WHERE c.group_id IS NULL AND (c.message LIKE ? OR p.name LIKE ? OR c.sha LIKE ?) ORDER BY c.id DESC LIMIT 51 OFFSET ?`, pattern, pattern, pattern, offset)
	if err != nil {
		return err
	}
	for rows.Next() {
		var t TicketCommit
		if err = rows.Scan(&t.ID, &t.RepositoryID, &t.GroupID, &t.Version, &t.SHA, &t.Branch, &t.Message, &t.Parents, &t.Reason, &t.RepositoryName, &t.Ticket); err != nil {
			rows.Close()
			return err
		}
		data.TicketCommits = append(data.TicketCommits, t)
	}
	err = rows.Err()
	rows.Close()
	pagination(c, data, offset, len(data.TicketCommits))
	if len(data.TicketCommits) > 50 {
		data.TicketCommits = data.TicketCommits[:50]
	}
	return err
}
