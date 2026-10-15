// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package platform

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func validateRepository(ctx context.Context, v *Repository) error {
	v.Name = strings.TrimSpace(v.Name)
	v.RemoteURL = strings.TrimRight(strings.TrimSpace(v.RemoteURL), "/")
	v.LocalPath = strings.TrimSpace(v.LocalPath)
	if v.Name == "" || len(v.Name) > 120 {
		return errors.New("repository name must be between 1 and 120 bytes")
	}
	if len(v.RemoteURL) > 2048 || len(v.LocalPath) > 4096 {
		return errors.New("repository location is too long")
	}
	if v.RemoteURL == "" && v.LocalPath == "" {
		return errors.New("provide a repository web URL or an existing local Git checkout")
	}
	if v.RemoteURL != "" {
		if err := validateURL(v.RemoteURL); err != nil {
			return err
		}
	}
	if v.LocalPath != "" {
		if !filepath.IsAbs(v.LocalPath) {
			return errors.New("local checkout path must be absolute")
		}
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "git", "-C", v.LocalPath, "rev-parse", "--show-toplevel")
		// Ambient Git overrides must not redirect validation to a different checkout.
		for _, entry := range os.Environ() {
			if !strings.HasPrefix(strings.ToUpper(entry), "GIT_") {
				cmd.Env = append(cmd.Env, entry)
			}
		}
		out, err := cmd.Output()
		if err != nil {
			return errors.New("local path must point to an accessible Git checkout and Git must be installed")
		}
		canonical, err := filepath.EvalSymlinks(strings.TrimSpace(string(out)))
		if err != nil {
			return errors.New("cannot resolve the local checkout path")
		}
		v.LocalPath = filepath.Clean(canonical)
		if runtime.GOOS == "windows" {
			v.LocalPath = strings.ToLower(v.LocalPath)
		}
	}
	return nil
}

func (s *Server) saveRepository(c *gin.Context) {
	v := Repository{Name: c.PostForm("name"), RemoteURL: c.PostForm("remote_url"), LocalPath: c.PostForm("local_path"), Version: version(c)}
	var source *GiteaSource
	var managedPath string
	if c.Param("id") != "" {
		id, err := recordID(c)
		if err != nil {
			s.fail(c, err)
			return
		}
		previous, err := s.store.Repository(c.Request.Context(), id)
		if err != nil {
			s.fail(c, err)
			return
		}
		v.ID, v.Archived = id, previous.Archived
		v.CreatedAt, v.UpdatedAt = previous.CreatedAt, previous.UpdatedAt
		source, err = s.store.GiteaSource(c.Request.Context(), id)
		if err != nil {
			s.fail(c, err)
			return
		}
		if source != nil {
			v.RemoteURL, managedPath, v.LocalPath = previous.RemoteURL, previous.LocalPath, ""
		}
	}
	if err := validateRepository(c.Request.Context(), &v); err != nil {
		if source != nil {
			v.LocalPath = managedPath
		}
		s.render(c, http.StatusUnprocessableEntity, page{Page: "repository", Title: s.text("Repository details"), Repository: v, Source: source, Error: err.Error()})
		return
	}
	if source != nil {
		v.LocalPath = managedPath
	}
	id, err := s.store.SaveRepository(c.Request.Context(), v)
	if err != nil {
		if errors.Is(err, ErrDuplicate) || errors.Is(err, ErrConflict) {
			s.render(c, http.StatusConflict, page{Page: "repository", Title: s.text("Repository details"), Repository: v, Source: source, Error: err.Error()})
		} else {
			s.fail(c, err)
		}
		return
	}
	c.Redirect(http.StatusSeeOther, fmt.Sprintf("/repositories/%d?saved=1", id))
}

func (s *Server) saveReview(c *gin.Context) {
	v := Review{Title: strings.TrimSpace(c.PostForm("title")), Background: strings.TrimSpace(c.PostForm("background")),
		BaseRef: strings.TrimSpace(c.PostForm("base_ref")), HeadRef: strings.TrimSpace(c.PostForm("head_ref")), Version: version(c)}
	if c.Param("id") != "" {
		id, err := recordID(c)
		if err != nil {
			s.fail(c, err)
			return
		}
		previous, err := s.store.Review(c.Request.Context(), id)
		if err != nil {
			s.fail(c, err)
			return
		}
		v.ID, v.RepositoryID, v.RepositoryName, v.Archived = id, previous.RepositoryID, previous.RepositoryName, previous.Archived
		v.CreatedAt, v.UpdatedAt = previous.CreatedAt, previous.UpdatedAt
	} else {
		id, err := strconv.ParseInt(c.PostForm("repository_id"), 10, 64)
		if err != nil || id < 1 {
			s.render(c, http.StatusBadRequest, page{Page: "error", Title: s.text("Select a repository"), Error: "Choose a repository before creating a review draft."})
			return
		}
		repo, err := s.store.Repository(c.Request.Context(), id)
		if err != nil {
			s.fail(c, err)
			return
		}
		if repo.Archived {
			s.fail(c, ErrConflict)
			return
		}
		v.RepositoryID, v.RepositoryName = id, repo.Name
	}
	message := ""
	if v.Title == "" || len(v.Title) > 200 {
		message = "review title must be between 1 and 200 bytes"
	}
	if len(v.Background) > 20000 || len(v.BaseRef) > 250 || len(v.HeadRef) > 250 {
		message = "background or revision name is too long"
	}
	if message != "" {
		s.render(c, http.StatusUnprocessableEntity, page{Page: "review", Title: s.text("Review draft"), Review: v, Error: message})
		return
	}
	id, err := s.store.SaveReview(c.Request.Context(), v)
	if err != nil {
		if errors.Is(err, ErrConflict) {
			s.render(c, http.StatusConflict, page{Page: "review", Title: s.text("Review draft"), Review: v, Error: err.Error()})
		} else {
			s.fail(c, err)
		}
		return
	}
	c.Redirect(http.StatusSeeOther, fmt.Sprintf("/reviews/%d?saved=1", id))
}
