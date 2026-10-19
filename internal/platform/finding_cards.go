// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

type FindingCard struct {
	Number                           int
	Title                            string
	Key, Path, Severity, Confidence  string
	Line                             int
	Description                      template.HTML
	Trigger, Expected, Chain, Impact string
	Sources                          []FindingSource
	RelatedSources                   []FindingSource
	Liked                            bool
	Assessment, Change               string
	Saved                            bool
	Comments                         []FindingComment
}

type FindingSource struct {
	Path, Revision string
	Lines          []FindingLine
}

type FindingLine struct {
	Number int
	Text   string
	Focus  bool
}

type FindingComment struct {
	ID, Version                   int64
	Content, CreatedAt, UpdatedAt string
}

func (s *Server) findingCards(ex *ReviewExecution) ([]FindingCard, error) {
	if ex == nil {
		return nil, nil
	}
	var cards []FindingCard
	seen := map[string]bool{}
	add := func(f businessFinding, code, revision string) error {
		key := findingKey(f.Path, f.Content)
		if seen[key] {
			return nil
		}
		seen[key] = true
		for _, verdict := range ex.Verdicts {
			if verdict.FindingID == f.ID {
				if verdict.State == "rejected" {
					return nil
				}
				f.Facts.Confidence = verdict.State
			}
		}
		description, err := renderReviewMarkdown(findingDescription(f.Content), s.text)
		if err != nil {
			return err
		}
		title, _, _ := strings.Cut(findingDescription(f.Content), "\n")
		title = strings.TrimSpace(strings.TrimLeft(title, "#* "))
		if runes := []rune(title); len(runes) > 120 {
			title = string(runes[:120]) + "..."
		}
		card := FindingCard{Number: len(cards) + 1, Title: title, Key: key, Path: f.Path, Line: f.Line, Severity: f.Severity, Confidence: f.Facts.Confidence, Description: description, Trigger: findingDescription(f.Facts.Trigger), Expected: findingDescription(f.Facts.Expected), Impact: findingDescription(f.Facts.Impact)}
		for _, p := range ex.Paths {
			if p.ID == f.Facts.PathID {
				card.Chain = findingDescription(p.Chain)
			}
		}
		// Locate the primary line independently: causal citations can point at callers in other files.
		for _, call := range ex.Calls {
			source := savedFindingSource(ex, sourceEvidence{LookupID: call.ID, Path: f.Path, Line: f.Line})
			if source.Path == f.Path && len(source.Lines) > 0 && (revision == "" || source.Revision == revision) && (ex.ContextSHA == "" || source.Revision == ex.ContextSHA || source.Revision == revision) {
				card.Sources = append(card.Sources, source)
				break
			}
		}
		for _, evidence := range f.Facts.Evidence {
			source := savedFindingSource(ex, evidence)
			if len(source.Lines) > 0 {
				card.RelatedSources = append(card.RelatedSources, source)
			}
		}
		if len(card.Sources) == 0 && code != "" {
			source := FindingSource{Path: f.Path, Revision: revision}
			for i, line := range strings.Split(strings.TrimSuffix(code, "\n"), "\n") {
				source.Lines = append(source.Lines, FindingLine{Number: f.Line + i, Text: line, Focus: true})
			}
			card.Sources = append([]FindingSource{source}, card.Sources...)
		}
		card.RelatedSources = relatedFindingSources(card.Sources, card.RelatedSources)
		cards = append(cards, card)
		return nil
	}
	for _, f := range ex.Findings {
		code, revision := "", ""
		for _, commit := range ex.Native {
			for _, cm := range commit.Comments {
				if findingKey(cm.Path, cm.Content) == findingKey(f.Path, f.Content) {
					code, revision = cm.ExistingCode, commit.Commit
				}
			}
		}
		if err := add(f, code, revision); err != nil {
			return nil, err
		}
	}
	if ex.Direct == nil {
		for _, commit := range ex.Native {
			for _, cm := range commit.Comments {
				if err := add(businessFinding{Path: cm.Path, Line: cm.StartLine, Severity: cm.Severity, Content: cm.Content}, cm.ExistingCode, commit.Commit); err != nil {
					return nil, err
				}
			}
		}
	}
	return cards, nil
}

func relatedFindingSources(primary, related []FindingSource) []FindingSource {
	var sources []FindingSource
	for _, source := range mergeFindingSources(related) {
		covered := false
		for _, main := range primary {
			if main.Path == source.Path && main.Revision == source.Revision && len(main.Lines) > 0 && main.Lines[0].Number <= source.Lines[0].Number && main.Lines[len(main.Lines)-1].Number >= source.Lines[len(source.Lines)-1].Number {
				covered = true
				break
			}
		}
		if !covered {
			sources = append(sources, source)
		}
	}
	return sources
}

func mergeFindingSources(sources []FindingSource) []FindingSource {
	var merged []FindingSource
	for _, source := range sources {
		if len(source.Lines) == 0 {
			continue
		}
		position := len(merged)
		for i := 0; i < len(merged); {
			other := merged[i]
			if source.Path != other.Path || source.Revision != other.Revision || source.Lines[0].Number > other.Lines[len(other.Lines)-1].Number || other.Lines[0].Number > source.Lines[len(source.Lines)-1].Number {
				i++
				continue
			}
			position = min(position, i)
			lines := make(map[int]FindingLine, len(source.Lines)+len(other.Lines))
			for _, line := range other.Lines {
				lines[line.Number] = line
			}
			for _, line := range source.Lines {
				line.Focus = line.Focus || lines[line.Number].Focus
				lines[line.Number] = line
			}
			source.Lines = make([]FindingLine, 0, len(lines))
			for _, line := range lines {
				source.Lines = append(source.Lines, line)
			}
			sort.Slice(source.Lines, func(i, j int) bool { return source.Lines[i].Number < source.Lines[j].Number })
			merged = append(merged[:i], merged[i+1:]...)
			// An expanded range can bridge a fragment visited earlier.
			i = 0
		}
		merged = append(merged, FindingSource{})
		copy(merged[position+1:], merged[position:])
		merged[position] = source
	}
	return merged
}

func savedFindingSource(ex *ReviewExecution, e sourceEvidence) FindingSource {
	for _, call := range ex.Calls {
		if call.ID != e.LookupID || call.Name != "file_read" || call.State != "completed" {
			continue
		}
		var args struct {
			Path string `json:"file_path"`
		}
		if json.Unmarshal([]byte(call.Arguments), &args) != nil || args.Path == "" || (e.Path != "" && e.Path != args.Path) {
			continue
		}
		lines := sourceReadLines(call.Result)
		end := max(e.Line, e.EndLine)
		if e.Line < 1 || end-e.Line >= 50 {
			return FindingSource{}
		}
		for n := e.Line; n <= end; n++ {
			if _, ok := lines[n]; !ok {
				return FindingSource{}
			}
		}
		source := FindingSource{Path: args.Path, Revision: call.Revision}
		for n := max(1, e.Line-3); n <= end+3; n++ {
			if text, ok := lines[n]; ok {
				source.Lines = append(source.Lines, FindingLine{Number: n, Text: text, Focus: n >= e.Line && n <= end})
			}
		}
		return source
	}
	return FindingSource{}
}

func (s *Server) loadFindingFeedback(ctx context.Context, job *ReviewJob) error {
	for i := range job.FindingCards {
		card := &job.FindingCards[i]
		if err := s.store.db.QueryRowContext(ctx, "SELECT COALESCE((SELECT assessment FROM finding_assessments WHERE job_id=? AND finding_key=?),'')", job.ID, card.Key).Scan(&card.Assessment); err != nil {
			return err
		}
		card.Liked = card.Assessment == "accurate"
		rows, err := s.store.db.QueryContext(ctx, "SELECT id,version,content,created_at,updated_at FROM finding_comments WHERE job_id=? AND finding_key=? ORDER BY id", job.ID, card.Key)
		if err != nil {
			return err
		}
		for rows.Next() {
			var comment FindingComment
			if err = rows.Scan(&comment.ID, &comment.Version, &comment.Content, &comment.CreatedAt, &comment.UpdatedAt); err != nil {
				rows.Close()
				return err
			}
			card.Comments = append(card.Comments, comment)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) findingFeedback(c *gin.Context, id int64) {
	job, err := s.resultJob(c.Request.Context(), id)
	if err != nil {
		s.fail(c, err)
		return
	}
	key := c.PostForm("finding_key")
	found := false
	for _, card := range job.FindingCards {
		found = found || card.Key == key
	}
	if !found {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	action := c.PostForm("job_action")
	if action == "like_finding" || action == "assess_finding" {
		assessment := c.PostForm("assessment")
		if action == "like_finding" {
			switch c.PostForm("liked") {
			case "1":
				assessment = "accurate"
			case "0":
				assessment = ""
			default:
				c.AbortWithStatus(http.StatusUnprocessableEntity)
				return
			}
		}
		switch assessment {
		case "", "accurate", "false_positive", "uncertain", "resolved":
			err = s.saveFindingAssessment(c.Request.Context(), id, key, assessment)
		default:
			c.AbortWithStatus(http.StatusUnprocessableEntity)
			return
		}
	} else if action == "delete_finding_comment" {
		commentID, _ := strconv.ParseInt(c.PostForm("comment_id"), 10, 64)
		version, _ := strconv.ParseInt(c.PostForm("comment_version"), 10, 64)
		result, e := s.store.db.ExecContext(c.Request.Context(), "DELETE FROM finding_comments WHERE id=? AND job_id=? AND finding_key=? AND version=?", commentID, id, key, version)
		err = changed(result, e)
	} else {
		content := strings.TrimSpace(c.PostForm("comment"))
		if content == "" || utf8.RuneCountInString(content) > 10000 {
			s.render(c, http.StatusUnprocessableEntity, page{Page: "error", Title: s.text("Invalid form"), Error: "Comment must contain 1-10000 characters."})
			return
		}
		if action == "edit_finding_comment" {
			commentID, _ := strconv.ParseInt(c.PostForm("comment_id"), 10, 64)
			version, _ := strconv.ParseInt(c.PostForm("comment_version"), 10, 64)
			result, e := s.store.db.ExecContext(c.Request.Context(), "UPDATE finding_comments SET content=?,updated_at=?,version=version+1 WHERE id=? AND job_id=? AND finding_key=? AND version=?", content, now(), commentID, id, key, version)
			err = changed(result, e)
		} else {
			_, err = s.store.db.ExecContext(c.Request.Context(), "INSERT INTO finding_comments(job_id,finding_key,content,created_at) VALUES(?,?,?,?)", id, key, content, now())
		}
	}
	if err != nil {
		s.fail(c, err)
		return
	}
	c.Redirect(http.StatusSeeOther, fmt.Sprintf("/reviews/runs/%d?saved=1&saved_finding=%s#finding-%s", id, key, key))
}

func (s *Server) saveFindingAssessment(ctx context.Context, id int64, key, assessment string) error {
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM finding_assessments WHERE job_id=? AND finding_key=?", id, key); err != nil {
		return err
	}
	if assessment != "" {
		if _, err = tx.ExecContext(ctx, "INSERT INTO finding_assessments(job_id,finding_key,assessment) VALUES(?,?,?)", id, key, assessment); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM finding_likes WHERE job_id=? AND finding_key=?", id, key); err != nil {
		return err
	}
	if assessment == "accurate" {
		if _, err = tx.ExecContext(ctx, "INSERT INTO finding_likes(job_id,finding_key) VALUES(?,?)", id, key); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Server) findingFeedbackText(ctx context.Context, id int64) (string, error) {
	job, err := s.resultJob(ctx, id)
	if err != nil {
		return "", err
	}
	var text strings.Builder
	for _, card := range job.FindingCards {
		if card.Liked {
			fmt.Fprintf(&text, "\nFinding %s:%d: user marked this finding accurate.\n", card.Path, card.Line)
		}
		switch card.Assessment {
		case "false_positive":
			fmt.Fprintf(&text, "\nFinding %s:%d: user considers this a false positive; reassess the evidence.\n", card.Path, card.Line)
		case "uncertain":
			fmt.Fprintf(&text, "\nFinding %s:%d: user is uncertain; verify the trigger and impact.\n", card.Path, card.Line)
		case "resolved":
			fmt.Fprintf(&text, "\nFinding %s:%d: user marked this resolved; verify against the current code.\n", card.Path, card.Line)
		}
		for _, comment := range card.Comments {
			fmt.Fprintf(&text, "\nFinding %s:%d user comment:\n%s\n", card.Path, card.Line, comment.Content)
		}
	}
	return text.String(), nil
}
