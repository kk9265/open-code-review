// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package platform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type ManagedRepository struct {
	ID                                      int64
	Name, FullName, State, CachePath, Error string
	Archived                                bool
	SyncMode                                string
}

func (s *Store) ManagedRepositories(ctx context.Context, dataDir string) ([]ManagedRepository, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT r.id,r.name,g.full_name,g.sync_state,g.sync_error,r.archived,COALESCE(t.sync_mode,'http')
		FROM repositories r JOIN gitea_repositories g ON g.repository_id=r.id LEFT JOIN repository_tracking t ON t.repository_id=r.id ORDER BY r.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []ManagedRepository{}
	for rows.Next() {
		var v ManagedRepository
		if err := rows.Scan(&v.ID, &v.Name, &v.FullName, &v.State, &v.Error, &v.Archived, &v.SyncMode); err != nil {
			return nil, err
		}
		v.CachePath = filepath.Join(dataDir, "repositories", strconv.FormatInt(v.ID, 10)+".git")
		values = append(values, v)
	}
	return values, rows.Err()
}

// The caller holds the data-directory lock for the entire maintenance operation.
func (s *Store) MaintainRepository(ctx context.Context, dataDir string, id int64, action, localSource string, output io.Writer) error {
	repo, err := s.Repository(ctx, id)
	if err != nil {
		return err
	}
	source, err := s.GiteaSource(ctx, id)
	if err != nil {
		return err
	}
	if source == nil {
		return errors.New("repository is not a managed Gitea repository")
	}
	if action == "branches" {
		root, err := managedDirectory(dataDir, "repositories")
		if err != nil {
			return err
		}
		path := filepath.Join(root, strconv.FormatInt(id, 10)+".git")
		info, err := os.Lstat(path)
		if err != nil {
			return errors.New("cache is unavailable; fetch this repository first")
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !samePath(path, resolved) {
			return errors.New("invalid managed cache path")
		}
		return runManagedGitWithOutput(ctx, nil, output, "--git-dir="+path, "for-each-ref", "--format=%(refname:short) %(objectname)", "refs/heads/")
	}
	if action != "fetch" {
		return errors.New("unsupported maintenance action")
	}
	if repo.Archived {
		return errors.New("restore the archived repository before fetching")
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE gitea_repositories SET sync_state='running',sync_error='' WHERE repository_id=?", id); err != nil {
		return err
	}
	app := &Server{config: Config{DataDir: dataDir}, store: s}
	var path, remoteURL, branch, fullName string
	var syncErr error
	if localSource != "" {
		path, branch, syncErr = syncLocalRepository(ctx, dataDir, id, localSource, output)
		remoteURL, fullName = repo.RemoteURL, source.FullName
	} else {
		var remote GiteaRepository
		path, remoteURL, remote, syncErr = app.configuredSync(ctx, source, output)
		branch, fullName = remote.DefaultBranch, remote.Owner.Login+"/"+remote.Name
	}
	if ctx.Err() != nil {
		syncErr = errSyncInterrupted
	}
	finishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.finishGiteaSync(finishCtx, id, path, branch, fullName, remoteURL, syncErr); err != nil {
		return fmt.Errorf("save sync result: %w", err)
	}
	app.trackingResult(ctx, id, path, syncErr)
	return syncErr
}
