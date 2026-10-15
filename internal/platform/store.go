// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package platform

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

var ErrConflict = errors.New("this record changed in another page; reload before saving")
var ErrDuplicate = errors.New("this repository is already registered; check active and archived repositories")

type Store struct{ db *sql.DB }

type Repository struct {
	ID, Version                int64
	Name, RemoteURL, LocalPath string
	CreatedAt, UpdatedAt       string
	Archived                   bool
}

type Review struct {
	ID, RepositoryID, Version            int64
	Title, Background, BaseRef, HeadRef  string
	RepositoryName, CreatedAt, UpdatedAt string
	Archived                             bool
}

type Counts struct{ Repositories, Drafts, Archived int }

func OpenStore(ctx context.Context, dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	path := filepath.ToSlash(filepath.Join(dir, "workbench.db"))
	if filepath.VolumeName(dir) != "" {
		path = "/" + path
	}
	u := url.URL{Scheme: "file", Path: path, RawQuery: "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{db: db}
	if err = s.initialize(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize database: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error                   { return s.db.Close() }
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *Store) initialize(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	files, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current int
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&current); err != nil {
		return err
	}
	if current > len(files) {
		return errors.New("database was created by a newer application version")
	}
	for i, name := range files {
		version, err := strconv.Atoi(strings.Split(filepath.Base(name), "_")[0])
		if err != nil || version != i+1 {
			return fmt.Errorf("invalid migration sequence: %s", name)
		}
		if version <= current {
			continue
		}
		body, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations VALUES (?, ?)", version, now()); err != nil {
			return err
		}
	}
	body, err := json.Marshal(defaultSettings())
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO settings(id, payload) VALUES (1, ?)", string(body)); err != nil {
		return err
	}
	return tx.Commit()
}

func now() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z") }

func (s *Store) Settings(ctx context.Context) (Settings, error) {
	var value Settings
	var body string
	if err := s.db.QueryRowContext(ctx, "SELECT payload, version FROM settings WHERE id=1").Scan(&body, &value.Version); err != nil {
		return value, err
	}
	err := json.Unmarshal([]byte(body), &value)
	return value, err
}

func (s *Store) SaveSettings(ctx context.Context, value Settings) error {
	if err := value.Validate(); err != nil {
		return err
	}
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, "UPDATE settings SET payload=?, version=version+1 WHERE id=1 AND version=?", string(body), value.Version)
	return changed(result, err)
}

func changed(result sql.Result, err error) error {
	if err != nil {
		var sqliteErr *sqlite.Error
		if errors.As(err, &sqliteErr) && (sqliteErr.Code() == 2067 || sqliteErr.Code() == 1555) {
			return ErrDuplicate
		}
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return ErrConflict
	}
	return err
}

func (s *Store) Counts(ctx context.Context) (Counts, error) {
	var c Counts
	err := s.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM repositories WHERE archived=0),
		(SELECT COUNT(*) FROM reviews WHERE archived=0),
		(SELECT COUNT(*) FROM reviews WHERE archived=1)`).Scan(&c.Repositories, &c.Drafts, &c.Archived)
	return c, err
}

func (s *Store) Repositories(ctx context.Context, archived bool, query string, offset int) ([]Repository, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,remote_url,local_path,archived,version,created_at,updated_at
		FROM repositories WHERE archived=? AND name LIKE ? ORDER BY updated_at DESC,id DESC LIMIT 51 OFFSET ?`, archived, "%"+query+"%", offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []Repository
	for rows.Next() {
		var v Repository
		if err := rows.Scan(&v.ID, &v.Name, &v.RemoteURL, &v.LocalPath, &v.Archived, &v.Version, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, rows.Err()
}

func (s *Store) Repository(ctx context.Context, id int64) (Repository, error) {
	var v Repository
	err := s.db.QueryRowContext(ctx, `SELECT id,name,remote_url,local_path,archived,version,created_at,updated_at FROM repositories WHERE id=?`, id).
		Scan(&v.ID, &v.Name, &v.RemoteURL, &v.LocalPath, &v.Archived, &v.Version, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}

func (s *Store) SaveRepository(ctx context.Context, v Repository) (int64, error) {
	if v.ID != 0 {
		result, err := s.db.ExecContext(ctx, `UPDATE repositories SET name=?,remote_url=?,local_path=?,version=version+1,updated_at=? WHERE id=? AND version=?`,
			v.Name, v.RemoteURL, v.LocalPath, now(), v.ID, v.Version)
		return v.ID, changed(result, err)
	}
	stamp := now()
	result, err := s.db.ExecContext(ctx, `INSERT INTO repositories(name,remote_url,local_path,created_at,updated_at) VALUES (?,?,?,?,?)`, v.Name, v.RemoteURL, v.LocalPath, stamp, stamp)
	if err := changed(result, err); err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (s *Store) Archive(ctx context.Context, kind string, id, version int64, archived bool) error {
	if kind != "repositories" && kind != "reviews" {
		return errors.New("invalid record kind")
	}
	result, err := s.db.ExecContext(ctx, "UPDATE "+kind+" SET archived=?,version=version+1,updated_at=? WHERE id=? AND version=?", archived, now(), id, version)
	return changed(result, err)
}

const reviewColumns = `r.id,r.repository_id,r.title,r.background,r.base_ref,r.head_ref,r.archived,r.version,r.created_at,r.updated_at,p.name`

func (s *Store) Reviews(ctx context.Context, archived bool, query string, offset int) ([]Review, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+reviewColumns+` FROM reviews r JOIN repositories p ON p.id=r.repository_id
		WHERE r.archived=? AND (r.title LIKE ? OR p.name LIKE ?) ORDER BY r.updated_at DESC,r.id DESC LIMIT 51 OFFSET ?`, archived, "%"+query+"%", "%"+query+"%", offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []Review
	for rows.Next() {
		v, err := scanReview(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, rows.Err()
}

func scanReview(row interface{ Scan(...any) error }) (Review, error) {
	var v Review
	err := row.Scan(&v.ID, &v.RepositoryID, &v.Title, &v.Background, &v.BaseRef, &v.HeadRef, &v.Archived, &v.Version, &v.CreatedAt, &v.UpdatedAt, &v.RepositoryName)
	return v, err
}

func (s *Store) Review(ctx context.Context, id int64) (Review, error) {
	return scanReview(s.db.QueryRowContext(ctx, `SELECT `+reviewColumns+` FROM reviews r JOIN repositories p ON p.id=r.repository_id WHERE r.id=?`, id))
}

func (s *Store) SaveReview(ctx context.Context, v Review) (int64, error) {
	if v.ID != 0 {
		result, err := s.db.ExecContext(ctx, `UPDATE reviews SET title=?,background=?,base_ref=?,head_ref=?,version=version+1,updated_at=? WHERE id=? AND version=?`,
			v.Title, v.Background, v.BaseRef, v.HeadRef, now(), v.ID, v.Version)
		return v.ID, changed(result, err)
	}
	stamp := now()
	result, err := s.db.ExecContext(ctx, `INSERT INTO reviews(repository_id,title,background,base_ref,head_ref,created_at,updated_at)
		SELECT id,?,?,?,?,?,? FROM repositories WHERE id=? AND archived=0`, v.Title, v.Background, v.BaseRef, v.HeadRef, stamp, stamp, v.RepositoryID)
	if err := changed(result, err); err != nil {
		return 0, err
	}
	return result.LastInsertId()
}
