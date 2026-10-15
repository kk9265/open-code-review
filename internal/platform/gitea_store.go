// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package platform

import (
	"context"
	"database/sql"
	"errors"
)

type GiteaSource struct {
	RepositoryID, RemoteID                                             int64
	ServerURL, FullName, DefaultBranch, SyncState, SyncError, SyncedAt string
}

func (s *Store) ImportGitea(ctx context.Context, settings Settings, remote GiteaRepository, remoteURL string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var id int64
	err = tx.QueryRowContext(ctx, "SELECT repository_id FROM gitea_repositories WHERE server_url=? AND remote_id=?", settings.GiteaURL, remote.ID).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	var version int64
	if err := tx.QueryRowContext(ctx, "SELECT version FROM settings WHERE id=1").Scan(&version); err != nil {
		return 0, err
	}
	if version != settings.Version {
		return 0, ErrConflict
	}
	stamp := now()
	name := remote.Owner.Login + "/" + remote.Name
	// A manually registered URL is reused only when it has no user checkout to replace.
	err = tx.QueryRowContext(ctx, "SELECT id FROM repositories WHERE remote_url=? AND local_path='' AND archived=0", remoteURL).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		res, err := tx.ExecContext(ctx, "INSERT INTO repositories(name,remote_url,created_at,updated_at) VALUES (?,?,?,?)", remote.Name, remoteURL, stamp, stamp)
		if err := changed(res, err); err != nil {
			return 0, err
		}
		id, err = res.LastInsertId()
		if err != nil {
			return 0, err
		}
	} else if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO gitea_repositories(repository_id,server_url,remote_id,full_name,default_branch) VALUES (?,?,?,?,?)", id, settings.GiteaURL, remote.ID, name, remote.DefaultBranch); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func (s *Store) GiteaSource(ctx context.Context, id int64) (*GiteaSource, error) {
	var v GiteaSource
	err := s.db.QueryRowContext(ctx, `SELECT repository_id,server_url,remote_id,full_name,default_branch,sync_state,sync_error,synced_at
		FROM gitea_repositories WHERE repository_id=?`, id).Scan(&v.RepositoryID, &v.ServerURL, &v.RemoteID, &v.FullName, &v.DefaultBranch, &v.SyncState, &v.SyncError, &v.SyncedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &v, err
}

func (s *Store) GiteaImports(ctx context.Context, server string) (map[int64]int64, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT remote_id,repository_id FROM gitea_repositories WHERE server_url=?", server)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make(map[int64]int64)
	for rows.Next() {
		var remote, local int64
		if err := rows.Scan(&remote, &local); err != nil {
			return nil, err
		}
		values[remote] = local
	}
	return values, rows.Err()
}

func (s *Store) QueueGiteaSync(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE gitea_repositories SET sync_state='queued',sync_error=''
		WHERE repository_id=? AND sync_state NOT IN ('queued','running') AND EXISTS (SELECT 1 FROM repositories WHERE id=? AND archived=0)`, id, id)
	return changed(res, err)
}

func (s *Store) nextGiteaSync(ctx context.Context) (*GiteaSource, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `UPDATE gitea_repositories SET sync_state='running',sync_error=''
		WHERE repository_id=(SELECT g.repository_id FROM gitea_repositories g JOIN repositories r ON r.id=g.repository_id WHERE g.sync_state='queued' AND r.archived=0 ORDER BY g.repository_id LIMIT 1)
		RETURNING repository_id`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.GiteaSource(ctx, id)
}

func (s *Store) finishGiteaSync(ctx context.Context, id int64, path, branch, fullName, remoteURL string, syncErr error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if syncErr != nil {
		state := "failed"
		if errors.Is(syncErr, errSyncInterrupted) {
			state = "interrupted"
		}
		_, err = tx.ExecContext(ctx, "UPDATE gitea_repositories SET sync_state=?,sync_error=? WHERE repository_id=?", state, syncErr.Error(), id)
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE gitea_repositories SET sync_state='ready',sync_error='',synced_at=?,default_branch=?,full_name=? WHERE repository_id=?", now(), branch, fullName, id)
		if err == nil {
			_, err = tx.ExecContext(ctx, "UPDATE repositories SET local_path=?,remote_url=?,version=version+1,updated_at=? WHERE id=?", path, remoteURL, now(), id)
		}
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) interruptGiteaSync(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "UPDATE gitea_repositories SET sync_state='interrupted',sync_error='Repository sync was interrupted. Retry to continue.' WHERE sync_state='running'")
	return err
}
