// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package platform

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func (s *Server) giteaRepositories(c *gin.Context) {
	settings, err := s.store.Settings(c.Request.Context())
	if err != nil {
		s.fail(c, err)
		return
	}
	n, _ := strconv.Atoi(c.Query("page"))
	if n < 1 || n > 100000 {
		n = 1
	}
	query := strings.TrimSpace(c.Query("q"))
	if len(query) > 200 {
		query = ""
	}
	data := page{Page: "gitea", Title: s.text("Gitea repositories"), Settings: settings, Query: query, GiteaApplicationsURL: settings.GiteaURL + "/user/settings/applications"}
	client, err := newGiteaClient(settings)
	if err != nil {
		data.Error = err.Error()
		s.render(c, http.StatusOK, data)
		return
	}
	data.TokenPresent = true
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	user, err := client.identity(ctx)
	if err != nil {
		data.Error = err.Error()
		s.render(c, http.StatusBadGateway, data)
		return
	}
	repositories, next, err := client.repositories(ctx, query, n)
	if err != nil {
		data.Error = err.Error()
		s.render(c, http.StatusBadGateway, data)
		return
	}
	imports, err := s.store.GiteaImports(ctx, settings.GiteaURL)
	if err != nil {
		s.fail(c, err)
		return
	}
	for i := range repositories {
		repositories[i].LocalID = imports[repositories[i].ID]
	}
	data.GiteaRepositories, data.GiteaLogin, data.GiteaConnected = repositories, user.Login, true
	link := func(page int) string {
		return "/gitea?" + url.Values{"page": {strconv.Itoa(page)}, "q": {query}}.Encode()
	}
	if n > 1 {
		data.Previous = link(n - 1)
	}
	if next {
		data.Next = link(n + 1)
	}
	s.render(c, http.StatusOK, data)
}

func (s *Server) importGitea(c *gin.Context) {
	id, err := strconv.ParseInt(c.PostForm("remote_id"), 10, 64)
	if err != nil || id < 1 {
		s.fail(c, ErrConflict)
		return
	}
	settings, err := s.store.Settings(c.Request.Context())
	if err != nil {
		s.fail(c, err)
		return
	}
	if settings.Version != version(c) {
		s.fail(c, ErrConflict)
		return
	}
	client, err := newGiteaClient(settings)
	if err != nil {
		s.giteaFailure(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	if _, err := client.identity(ctx); err != nil {
		s.giteaFailure(c, err)
		return
	}
	repo, err := client.repository(ctx, id)
	if err != nil {
		s.giteaFailure(c, err)
		return
	}
	if repo.Empty {
		s.giteaFailure(c, errors.New("This repository has no commits yet."))
		return
	}
	id, err = s.store.ImportGitea(ctx, settings, repo, client.repositoryURL(repo))
	if err != nil {
		s.fail(c, err)
		return
	}
	s.wakeRepositorySync()
	c.Redirect(http.StatusSeeOther, fmt.Sprintf("/repositories/%d", id))
}

func (s *Server) queueGiteaSync(c *gin.Context) {
	id, err := recordID(c)
	if err != nil {
		s.fail(c, err)
		return
	}
	if err := s.store.QueueGiteaSync(c.Request.Context(), id); err != nil {
		s.fail(c, err)
		return
	}
	s.wakeRepositorySync()
	c.Redirect(http.StatusSeeOther, fmt.Sprintf("/repositories/%d", id))
}

func (s *Server) giteaFailure(c *gin.Context, err error) {
	s.render(c, http.StatusBadGateway, page{Page: "error", Title: s.text("Gitea connection failed"), Error: err.Error()})
}
