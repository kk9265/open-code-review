// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package platform

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type Tracking struct {
	RepositoryID                                                                int64
	Branch, Mode, LocalSource, State, Baseline, LastSHA, LastChecked, LastError string
	Interval, Version, CommitCount                                              int
}

func (s *Store) Tracking(ctx context.Context, id int64) (*Tracking, error) {
	v := &Tracking{RepositoryID: id}
	err := s.db.QueryRowContext(ctx, `SELECT branch,sync_mode,local_source,interval_seconds,state,baseline_sha,last_sha,last_checked,last_error,version,
		(SELECT COUNT(*) FROM tracked_commits WHERE repository_id=?) FROM repository_tracking WHERE repository_id=?`, id, id).
		Scan(&v.Branch, &v.Mode, &v.LocalSource, &v.Interval, &v.State, &v.Baseline, &v.LastSHA, &v.LastChecked, &v.LastError, &v.Version, &v.CommitCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (s *Store) queueTrackedRepositories(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stamp := time.Now().Unix()
	_, err = tx.ExecContext(ctx, `UPDATE gitea_repositories SET sync_state='queued',sync_error=''
		WHERE sync_state NOT IN ('running','queued') AND repository_id IN
		(SELECT t.repository_id FROM repository_tracking t JOIN repositories r ON r.id=t.repository_id
		WHERE t.state IN ('pending','active') AND t.next_check<=? AND r.archived=0)`, stamp)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE repository_tracking SET next_check=?+interval_seconds
		WHERE next_check<=? AND state IN ('pending','active') AND repository_id IN (SELECT repository_id FROM gitea_repositories WHERE sync_state='queued')`, stamp, stamp)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func trackingGit(ctx context.Context, path string, args ...string) (string, error) {
	var output bytes.Buffer
	command := append([]string{"--git-dir=" + path}, args...)
	err := runManagedGitWithOutput(ctx, nil, &output, command...)
	return strings.TrimSpace(output.String()), err
}

func (s *Server) cachePath(id int64) (string, error) {
	root, err := managedDirectory(s.config.DataDir, "repositories")
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, strconv.FormatInt(id, 10)+".git")
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || !samePath(path, resolved) {
		return "", errors.New("Synchronize the repository before selecting a branch.")
	}
	return path, nil
}

func (s *Server) trackingBranches(ctx context.Context, id int64) ([]string, error) {
	path, err := s.cachePath(id)
	if err != nil {
		return nil, err
	}
	text, err := trackingGit(ctx, path, "for-each-ref", "--format=%(refname:strip=2)", "refs/heads/")
	if err != nil {
		return nil, err
	}
	if text == "" {
		return nil, nil
	}
	return strings.Split(text, "\n"), nil
}

func (s *Server) observeTracking(ctx context.Context, id int64, path string) error {
	t, err := s.store.Tracking(ctx, id)
	if err != nil || t == nil {
		return err
	}
	if t.State != "pending" && t.State != "active" {
		return nil
	}
	ref := "refs/heads/" + t.Branch
	head, err := trackingGit(ctx, path, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return errors.New("The tracked branch is missing. Check the branch or synchronize again.")
	}
	commits := []string{}
	if t.Baseline != "" && t.LastSHA != head {
		if _, err := trackingGit(ctx, path, "merge-base", "--is-ancestor", t.LastSHA, head); err != nil {
			_, saveErr := s.store.db.ExecContext(ctx, `UPDATE repository_tracking SET state='blocked',last_error=?,last_checked=?,version=version+1 WHERE repository_id=?`,
				"Branch history changed or its previous commit is unavailable. Tracking is blocked; the baseline was preserved.", now(), id)
			return saveErr
		}
		text, err := trackingGit(ctx, path, "rev-list", "--reverse", "--topo-order", t.LastSHA+".."+head, "--")
		if err != nil {
			return errors.New("Cannot enumerate new commits. Tracking will retry without advancing its cursor.")
		}
		if text != "" {
			commits = strings.Fields(text)
		}
	}
	baseline := t.Baseline
	if baseline == "" {
		baseline = head
	}
	// Pin the starting point and cursor so background Git housekeeping cannot remove them.
	prefix := "refs/ocr/tracking/" + strconv.FormatInt(id, 10) + "/"
	if err := runManagedGit(ctx, nil, "--git-dir="+path, "update-ref", prefix+"baseline", baseline); err != nil {
		return err
	}
	if err := runManagedGit(ctx, nil, "--git-dir="+path, "update-ref", prefix+"cursor", head); err != nil {
		return err
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stamp := now()
	for _, sha := range commits {
		if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO tracked_commits(repository_id,sha,discovered_at) VALUES (?,?,?)", id, sha, stamp); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE repository_tracking SET baseline_sha=?,last_sha=?,state='active',last_checked=?,last_error='',next_check=?+interval_seconds,version=version+1 WHERE repository_id=?`, baseline, head, stamp, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Server) trackingResult(ctx context.Context, id int64, path string, syncErr error) {
	err := syncErr
	if err == nil {
		err = s.observeTracking(ctx, id, path)
	}
	if err != nil {
		_, saveErr := s.store.db.ExecContext(ctx, `UPDATE repository_tracking SET last_error=?,last_checked=?,next_check=?+interval_seconds WHERE repository_id=? AND state IN ('pending','active')`, err.Error(), now(), time.Now().Unix(), id)
		if saveErr != nil && s.log != nil {
			s.log.Error("save tracking result", "repository_id", id, "error", saveErr)
		}
	}
}

func (s *Server) configuredSync(ctx context.Context, source *GiteaSource, output io.Writer) (string, string, GiteaRepository, error) {
	t, err := s.store.Tracking(ctx, source.RepositoryID)
	if err != nil {
		return "", "", GiteaRepository{}, err
	}
	if t == nil || t.Mode != "local" {
		return s.syncGiteaWithProgress(ctx, source, output)
	}
	repo, err := s.store.Repository(ctx, source.RepositoryID)
	if err != nil {
		return "", "", GiteaRepository{}, err
	}
	if output == nil {
		output = io.Discard
	}
	path, branch, err := syncLocalRepository(ctx, s.config.DataDir, source.RepositoryID, t.LocalSource, output)
	remote := GiteaRepository{DefaultBranch: branch, Name: source.FullName}
	parts := strings.SplitN(source.FullName, "/", 2)
	if len(parts) == 2 {
		remote.Owner.Login, remote.Name = parts[0], parts[1]
	}
	return path, repo.RemoteURL, remote, err
}

func (s *Server) saveTracking(c *gin.Context) {
	id, err := recordID(c)
	if err != nil {
		s.fail(c, err)
		return
	}
	if !s.syncMu.TryLock() {
		s.trackingFailure(c, id, "A repository sync is running. Try again after it finishes.")
		return
	}
	defer s.syncMu.Unlock()
	ctx := c.Request.Context()
	repo, err := s.store.Repository(ctx, id)
	if err != nil {
		s.fail(c, err)
		return
	}
	source, err := s.store.GiteaSource(ctx, id)
	if err != nil {
		s.fail(c, err)
		return
	}
	if repo.Archived || source == nil {
		s.trackingFailure(c, id, "Only active managed repositories can be tracked.")
		return
	}
	t, err := s.store.Tracking(ctx, id)
	if err != nil {
		s.fail(c, err)
		return
	}
	posted, _ := strconv.Atoi(c.PostForm("tracking_version"))
	if (t == nil && posted != 0) || (t != nil && t.Version != posted) {
		s.fail(c, ErrConflict)
		return
	}
	action := c.PostForm("tracking_action")
	if action == "pause" || action == "resume" {
		if t == nil {
			s.fail(c, ErrConflict)
			return
		}
		state := "paused"
		if action == "resume" {
			state = "active"
			if t.Baseline == "" {
				state = "pending"
			}
		}
		_, err = s.store.db.ExecContext(ctx, `UPDATE repository_tracking SET state=?,last_error='',next_check=0,version=version+1 WHERE repository_id=?`, state, id)
	} else if action == "start" || action == "save" {
		branch, mode, local := strings.TrimSpace(c.PostForm("branch")), c.PostForm("sync_mode"), strings.TrimSpace(c.PostForm("local_source"))
		interval, parseErr := strconv.Atoi(c.PostForm("interval_seconds"))
		if parseErr != nil || interval < 60 || interval > 3600 || (mode != "http" && mode != "local") {
			s.trackingFailure(c, id, "Choose a sync method and an interval between 60 and 3600 seconds.")
			return
		}
		branches, branchErr := s.trackingBranches(ctx, id)
		found := false
		for _, name := range branches {
			if name == branch {
				found = true
			}
		}
		if branchErr != nil || !found {
			s.trackingFailure(c, id, "Choose an existing cached branch. Synchronize first if it is missing.")
			return
		}
		if t != nil && t.Branch != branch {
			s.trackingFailure(c, id, "The tracked branch cannot be changed after tracking is configured.")
			return
		}
		if mode == "local" {
			if !filepath.IsAbs(local) {
				s.trackingFailure(c, id, "Choose an absolute path to a local bare repository.")
				return
			}
			local, err = filepath.EvalSymlinks(local)
			if err != nil {
				s.trackingFailure(c, id, "Choose an absolute path to a local bare repository.")
				return
			}
			cacheRoot, _ := filepath.Abs(filepath.Join(s.config.DataDir, "repositories"))
			if containsRepositoryPath(cacheRoot, local) || containsRepositoryPath(local, cacheRoot) {
				s.trackingFailure(c, id, "The local source must be separate from the workbench cache.")
				return
			}
			var bare bytes.Buffer
			if err := runManagedGitWithOutput(ctx, nil, &bare, "-c", "safe.directory="+local, "--git-dir="+local, "rev-parse", "--is-bare-repository"); err != nil || strings.TrimSpace(bare.String()) != "true" {
				s.trackingFailure(c, id, "Choose an absolute path to a local bare repository.")
				return
			}
		} else {
			local = ""
		}
		if t == nil {
			_, err = s.store.db.ExecContext(ctx, `INSERT INTO repository_tracking(repository_id,branch,sync_mode,local_source,interval_seconds,state) VALUES (?,?,?,?,?,'pending')`, id, branch, mode, local, interval)
		} else {
			_, err = s.store.db.ExecContext(ctx, `UPDATE repository_tracking SET sync_mode=?,local_source=?,interval_seconds=?,next_check=0,version=version+1 WHERE repository_id=?`, mode, local, interval, id)
		}
	} else {
		s.fail(c, ErrConflict)
		return
	}
	if err != nil {
		s.fail(c, err)
		return
	}
	s.wakeRepositorySync()
	c.Redirect(http.StatusSeeOther, fmt.Sprintf("/repositories/%d?saved=1", id))
}

func (s *Server) trackingFailure(c *gin.Context, id int64, message string) {
	c.Redirect(http.StatusSeeOther, fmt.Sprintf("/repositories/%d?tracking_error=%s", id, trackingErrorCode(message)))
}

var trackingErrors = []string{
	"A repository sync is running. Try again after it finishes.",
	"Only active managed repositories can be tracked.",
	"Choose a sync method and an interval between 60 and 3600 seconds.",
	"Choose an existing cached branch. Synchronize first if it is missing.",
	"The tracked branch cannot be changed after tracking is configured.",
	"Choose an absolute path to a local bare repository.",
	"The local source must be separate from the workbench cache.",
}

func trackingErrorCode(message string) string {
	for i, text := range trackingErrors {
		if text == message {
			return strconv.Itoa(i + 1)
		}
	}
	return "0"
}

func (s *Server) recentTrackedCommits(ctx context.Context, id int64) ([]string, error) {
	rows, err := s.store.db.QueryContext(ctx, "SELECT sha FROM tracked_commits WHERE repository_id=? ORDER BY discovered_at DESC,rowid DESC LIMIT 20", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []string
	for rows.Next() {
		var sha string
		if err := rows.Scan(&sha); err != nil {
			return nil, err
		}
		values = append(values, sha)
	}
	return values, rows.Err()
}
