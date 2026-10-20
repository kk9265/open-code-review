// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package platform

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var wikiSnapshotID = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (s *Server) wikiPlanner(c *gin.Context) {
	scope, base, ok := wikiScope(c)
	if !ok {
		return
	}
	wikiHeaders(c)
	path := c.Param("path")
	static := map[string]string{"/": "index.html", "/planning": "planning.html", "/app.js": "app.js", "/catalog.js": "catalog.js", "/style.css": "style.css", "/i18n.json": "i18n/zh-CN.json"}
	if name, ok := static[path]; ok {
		wikiAsset(c, name, base+"/plan")
		return
	}
	if path != "/api/snapshots" && !strings.HasPrefix(path, "/api/snapshots/") {
		c.Status(404)
		return
	}
	result, err := s.wikiScopeSnapshots(c.Request.Context(), strings.TrimPrefix(path, "/api/snapshots"), scope)
	if err != nil {
		c.JSON(422, gin.H{"error": s.text("Wiki output is not ready or failed integrity verification.")})
		return
	}
	c.JSON(200, result)
}

func (s *Server) wikiSnapshots(ctx context.Context, suffix string) (any, error) {
	return s.wikiScopeSnapshots(ctx, suffix, "loginserver")
}

func (s *Server) wikiScopeSnapshots(ctx context.Context, suffix, service string) (any, error) {
	root := filepath.Join(s.config.DataDir, "wiki-code-plan")
	if service == "accountserver" {
		root = filepath.Join(s.config.DataDir, "wiki-accountserver-plan")
	}
	database := filepath.Join(root, "wiki-preview.sqlite3")
	if _, err := os.Stat(database); err != nil {
		if os.IsNotExist(err) && suffix == "" {
			return []any{}, nil
		}
		return nil, err
	}
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(database), RawQuery: "mode=ro&_pragma=query_only(1)"}
	if !strings.HasPrefix(uri.Path, "/") {
		uri.Path = "/" + uri.Path
	}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if suffix == "" {
		rows, err := db.QueryContext(ctx, `SELECT s.id,s.repo_id,s.branch,s.commit_sha,s.created_at, EXISTS(SELECT 1 FROM current_previews c WHERE c.snapshot_id=s.id) FROM snapshots s ORDER BY s.created_at DESC,s.rowid DESC`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		result := []gin.H{}
		for rows.Next() {
			var id, repo, branch, commit string
			var created float64
			var current int
			if err := rows.Scan(&id, &repo, &branch, &commit, &created, &current); err != nil {
				return nil, err
			}
			result = append(result, gin.H{"id": id, "repo_id": repo, "branch": branch, "commit_sha": commit, "created_at": created, "is_current": current})
		}
		return result, rows.Err()
	}
	id := strings.TrimPrefix(suffix, "/")
	if !wikiSnapshotID.MatchString(id) {
		return nil, fmt.Errorf("invalid snapshot identity")
	}
	var artifact, hash, input string
	if err := db.QueryRowContext(ctx, `SELECT s.artifact,s.artifact_hash,j.inputs FROM snapshots s JOIN jobs j ON j.id=s.job_id WHERE s.id=?`, id).Scan(&artifact, &hash, &input); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(artifact) {
		artifact = filepath.Join(root, artifact)
	}
	relative, err := filepath.Rel(filepath.Join(root, "snapshots"), artifact)
	if err != nil {
		return nil, err
	}
	body, err := wikiRead(filepath.Join(root, "snapshots"), relative)
	if err != nil {
		return nil, err
	}
	if fmt.Sprintf("%x", sha256.Sum256(body)) != hash {
		return nil, fmt.Errorf("snapshot integrity check failed")
	}
	var payload, inputs map[string]any
	if err = json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(input), &inputs); err != nil {
		return nil, err
	}
	if payload["mode"] != "preview_only" || payload["generation_enabled"] != false || payload["model_calls"] != float64(0) || (payload["schema"] != float64(1) && payload["schema"] != float64(2) && payload["schema"] != float64(3)) {
		return nil, fmt.Errorf("unsupported preview")
	}
	catalog, ok := inputs["catalog"]
	if !ok {
		catalog = []any{}
	}
	scope, ok := inputs["scope"]
	if !ok {
		scope = []any{}
	}
	repository, ok := inputs["repo_path"]
	if !ok {
		repository = payload["repo_id"]
	}
	return gin.H{"snapshot_id": id, "preview": payload, "catalog": catalog, "scope": scope, "repository": repository}, nil
}
