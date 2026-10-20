// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package platform

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

//go:embed ui/*.html ui/static/* ui/i18n/*.json
var assets embed.FS

type Server struct {
	config   Config
	store    *Store
	tmpl     *template.Template
	csrf     string
	log      *slog.Logger
	text     func(string) string
	syncWake chan struct{}
	syncMu   sync.Mutex
}

type page struct {
	WikiScope, WikiPrefix                                    string
	Page, Title, Error, CSRF, Query, Previous, Next, DataDir string
	Saved, Archived, TokenPresent, ModelKeyPresent           bool
	Counts                                                   Counts
	Repositories                                             []Repository
	Reviews                                                  []Review
	Repository                                               Repository
	Review                                                   Review
	Settings                                                 Settings
	Source                                                   *GiteaSource
	RepositorySources                                        map[int64]*GiteaSource
	GiteaRepositories                                        []GiteaRepository
	GiteaLogin, GiteaApplicationsURL                         string
	GiteaConnected                                           bool
	Tracking                                                 *Tracking
	Branches, TrackedCommits                                 []string
	TrackingError                                            string
	QueueEnabled                                             bool
	QueueStatus                                              string
	Jobs                                                     []ReviewJob
	Job                                                      ReviewJob
	TicketCommits                                            []TicketCommit
	ResultGroups                                             []ResultGroup
	Unclassified                                             bool
	UnclassifiedCount                                        int
}

func NewServer(config Config, store *Store, logger *slog.Logger) (*Server, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	locale, err := assets.ReadFile("ui/i18n/zh-CN.json")
	if err != nil {
		return nil, err
	}
	var messages map[string]string
	if err := json.Unmarshal(locale, &messages); err != nil {
		return nil, fmt.Errorf("load UI messages: %w", err)
	}
	text := func(key string) string {
		if value, ok := messages[key]; ok {
			return value
		}
		return key
	}
	tmpl, err := template.New("app.html").Funcs(template.FuncMap{"t": text, "date": func(raw string) string {
		t, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return raw
		}
		return t.Local().Format("2006-01-02 15:04")
	}}).ParseFS(assets, "ui/*.html")
	if err != nil {
		return nil, err
	}
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{config: config, store: store, tmpl: tmpl, csrf: hex.EncodeToString(token), log: logger, text: text, syncWake: make(chan struct{}, 1)}, nil
}

func (s *Server) Handler() http.Handler {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	_ = r.SetTrustedProxies(nil)
	r.HandleMethodNotAllowed = true
	r.Use(s.protect(), gin.CustomRecovery(func(c *gin.Context, recovered any) {
		s.log.Error("request panic", "method", c.Request.Method, "path", c.Request.URL.Path)
		c.AbortWithStatus(http.StatusInternalServerError)
	}))
	r.GET("/healthz", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := s.store.Ping(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "storage": "sqlite"})
	})
	static, _ := fs.Sub(assets, "ui/static")
	r.StaticFS("/static", http.FS(static))
	r.GET("/", s.overview)
	r.GET("/wiki", s.wikiPage)
	r.GET("/wiki/read/*path", s.wikiReader)
	r.GET("/wiki/plan/*path", s.wikiPlanner)
	r.GET("/wiki/scopes/:scope/read/*path", s.wikiReader)
	r.GET("/wiki/scopes/:scope/plan/*path", s.wikiPlanner)
	r.GET("/repositories", s.repositories)
	r.POST("/repositories", s.saveRepository)
	r.GET("/repositories/import", s.giteaRepositories)
	r.POST("/repositories/import", s.importGitea)
	r.GET("/repositories/:id", s.repository)
	r.POST("/repositories/:id", s.saveRepository)
	r.POST("/repositories/:id/archive", s.archive("repositories"))
	r.POST("/repositories/:id/sync", s.queueGiteaSync)
	r.POST("/repositories/:id/tracking", s.saveTracking)
	r.GET("/gitea", func(c *gin.Context) {
		target := "/repositories/import"
		if c.Request.URL.RawQuery != "" {
			target += "?" + c.Request.URL.RawQuery
		}
		c.Redirect(http.StatusFound, target)
	})
	r.POST("/gitea/repositories", s.importGitea)
	r.GET("/reviews", s.resultsPage)
	r.GET("/reviews/drafts", s.reviews)
	r.GET("/reviews/new", s.newReview)
	r.GET("/reviews/queue", s.queuePage)
	r.GET("/reviews/queue/runs/:id", s.jobPage)
	r.POST("/reviews/queue", s.queueControl)
	r.GET("/reviews/runs/:id", s.jobPage)
	r.POST("/reviews/runs/:id", s.jobAction)
	r.POST("/reviews/commits/:id", s.assignTicket)
	r.POST("/reviews", s.saveReview)
	r.GET("/reviews/:id", s.review)
	r.POST("/reviews/:id", s.saveReview)
	r.POST("/reviews/:id/archive", s.archive("reviews"))
	r.GET("/settings", s.settings)
	r.POST("/settings", s.saveSettings)
	r.NoRoute(func(c *gin.Context) {
		s.render(c, http.StatusNotFound, page{Page: "error", Title: s.text("Page not found"), Error: "This page does not exist."})
	})
	r.NoMethod(func(c *gin.Context) {
		s.render(c, http.StatusMethodNotAllowed, page{Page: "error", Title: s.text("Method Not Allowed"), Error: "This request method is not supported."})
	})
	return r
}

func (s *Server) protect() gin.HandlerFunc {
	_, port, _ := net.SplitHostPort(s.config.Addr)
	return func(c *gin.Context) {
		host, requestPort, err := net.SplitHostPort(c.Request.Host)
		ip := net.ParseIP(host)
		if err != nil || requestPort != port || (host != "localhost" && (ip == nil || !ip.IsLoopback())) {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "same-origin")
		c.Header("Cache-Control", "no-store")
		c.Header("Content-Security-Policy", "default-src 'self'; script-src 'none'; style-src 'self'; img-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			if c.GetHeader("Sec-Fetch-Site") == "cross-site" {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			if origin := c.GetHeader("Origin"); origin != "" && origin != "http://"+c.Request.Host {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
			if err := c.Request.ParseForm(); err != nil {
				s.render(c, http.StatusBadRequest, page{Page: "error", Title: s.text("Invalid form"), Error: "The form could not be read or exceeded the size limit."})
				c.Abort()
				return
			}
			if subtle.ConstantTimeCompare([]byte(c.Request.PostForm.Get("csrf")), []byte(s.csrf)) != 1 {
				s.render(c, http.StatusForbidden, page{Page: "error", Title: s.text("Refresh this page"), Error: "The form has expired. Reload the page and try again."})
				c.Abort()
				return
			}
		}
		c.Next()
	}
}

func (s *Server) render(c *gin.Context, status int, data page) {
	data.CSRF = s.csrf
	data.Error = s.text(data.Error)
	data.DataDir = s.config.DataDir
	data.Saved = c.Query("saved") == "1" && data.Error == ""
	for i := range data.Job.FindingCards {
		data.Job.FindingCards[i].Saved = data.Saved && c.Query("saved_finding") == data.Job.FindingCards[i].Key
	}
	var body bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&body, "app.html", data); err != nil {
		s.log.Error("render page", "error", err)
		c.String(http.StatusInternalServerError, s.text("Unable to render this page."))
		return
	}
	c.Data(status, "text/html; charset=utf-8", body.Bytes())
}

func (s *Server) fail(c *gin.Context, err error) {
	status, message := http.StatusInternalServerError, "The operation could not be completed. Your existing records are unchanged."
	switch {
	case errors.Is(err, sql.ErrNoRows):
		status, message = http.StatusNotFound, "This record does not exist."
	case errors.Is(err, ErrConflict), errors.Is(err, ErrDuplicate):
		status, message = http.StatusConflict, err.Error()
	default:
		s.log.Error("storage operation", "path", c.Request.URL.Path, "error", err)
	}
	s.render(c, status, page{Page: "error", Title: s.text(http.StatusText(status)), Error: message})
}

func recordID(c *gin.Context) (int64, error) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		return 0, sql.ErrNoRows
	}
	return id, nil
}

func version(c *gin.Context) int64 {
	n, _ := strconv.ParseInt(c.PostForm("version"), 10, 64)
	return n
}

func listParams(c *gin.Context) (bool, string, int) {
	n, _ := strconv.Atoi(c.Query("page"))
	if n < 1 || n > 100000 {
		n = 1
	}
	query := c.Query("q")
	if len(query) > 200 {
		query = ""
	}
	return c.Query("archived") == "1", strings.TrimSpace(query), (n - 1) * 50
}

func pagination(c *gin.Context, data *page, offset, size int) {
	link := func(n int) string {
		q := url.Values{"page": {strconv.Itoa(n)}, "q": {data.Query}}
		if data.Archived {
			q.Set("archived", "1")
		}
		return c.Request.URL.Path + "?" + q.Encode()
	}
	if offset > 0 {
		data.Previous = link(offset / 50)
	}
	if size > 50 {
		data.Next = link(offset/50 + 2)
	}
}

func (s *Server) overview(c *gin.Context) {
	counts, err := s.store.Counts(c.Request.Context())
	if err != nil {
		s.fail(c, err)
		return
	}
	reviews, err := s.store.Reviews(c.Request.Context(), false, "", 0)
	if err != nil {
		s.fail(c, err)
		return
	}
	if len(reviews) > 5 {
		reviews = reviews[:5]
	}
	s.render(c, http.StatusOK, page{Page: "overview", Title: s.text("Overview"), Counts: counts, Reviews: reviews})
}

func (s *Server) repositories(c *gin.Context) {
	archived, query, offset := listParams(c)
	values, err := s.store.Repositories(c.Request.Context(), archived, query, offset)
	if err != nil {
		s.fail(c, err)
		return
	}
	data := page{Page: "repositories", Title: s.text("Repositories"), Repositories: values, Archived: archived, Query: query}
	pagination(c, &data, offset, len(values))
	if len(data.Repositories) > 50 {
		data.Repositories = data.Repositories[:50]
	}
	data.RepositorySources = make(map[int64]*GiteaSource, len(data.Repositories))
	for _, repository := range data.Repositories {
		source, err := s.store.GiteaSource(c.Request.Context(), repository.ID)
		if err != nil {
			s.fail(c, err)
			return
		}
		data.RepositorySources[repository.ID] = source
	}
	s.render(c, http.StatusOK, data)
}

func (s *Server) repository(c *gin.Context) {
	id, err := recordID(c)
	if err != nil {
		s.fail(c, err)
		return
	}
	v, err := s.store.Repository(c.Request.Context(), id)
	if err != nil {
		s.fail(c, err)
		return
	}
	source, err := s.store.GiteaSource(c.Request.Context(), id)
	if err != nil {
		s.fail(c, err)
		return
	}
	data := page{Page: "repository", Title: v.Name, Repository: v, Source: source}
	if source != nil {
		data.Tracking, err = s.store.Tracking(c.Request.Context(), id)
		if err != nil {
			s.fail(c, err)
			return
		}
		data.Branches, err = s.trackingBranches(c.Request.Context(), id)
		if err != nil {
			data.TrackingError = "Synchronize the repository before selecting a branch."
		}
		data.TrackedCommits, err = s.recentTrackedCommits(c.Request.Context(), id)
		if err != nil {
			s.fail(c, err)
			return
		}
		code, _ := strconv.Atoi(c.Query("tracking_error"))
		if code > 0 && code <= len(trackingErrors) {
			data.TrackingError = trackingErrors[code-1]
		}
	}
	s.render(c, http.StatusOK, data)
}

func (s *Server) reviews(c *gin.Context) {
	archived, query, offset := listParams(c)
	values, err := s.store.Reviews(c.Request.Context(), archived, query, offset)
	if err != nil {
		s.fail(c, err)
		return
	}
	data := page{Page: "reviews", Title: s.text("Reviews"), Reviews: values, Archived: archived, Query: query}
	pagination(c, &data, offset, len(values))
	if len(data.Reviews) > 50 {
		data.Reviews = data.Reviews[:50]
	}
	s.render(c, http.StatusOK, data)
}

func (s *Server) newReview(c *gin.Context) {
	id, err := strconv.ParseInt(c.Query("repository"), 10, 64)
	if err != nil || id < 1 {
		c.Redirect(http.StatusSeeOther, "/repositories")
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
	s.render(c, http.StatusOK, page{Page: "review", Title: s.text("New review"), Review: Review{RepositoryID: id, RepositoryName: repo.Name}})
}

func (s *Server) review(c *gin.Context) {
	id, err := recordID(c)
	if err != nil {
		s.fail(c, err)
		return
	}
	v, err := s.store.Review(c.Request.Context(), id)
	if err != nil {
		s.fail(c, err)
		return
	}
	s.render(c, http.StatusOK, page{Page: "review", Title: v.Title, Review: v})
}

func (s *Server) archive(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := recordID(c)
		if err != nil {
			s.fail(c, err)
			return
		}
		value := c.PostForm("archived")
		if value != "0" && value != "1" {
			s.fail(c, ErrConflict)
			return
		}
		if err := s.store.Archive(c.Request.Context(), kind, id, version(c), value == "1"); err != nil {
			s.fail(c, err)
			return
		}
		c.Redirect(http.StatusSeeOther, fmt.Sprintf("/%s/%d?saved=1", kind, id))
	}
}

func (s *Server) settings(c *gin.Context) {
	v, err := s.store.Settings(c.Request.Context())
	if err != nil {
		s.fail(c, err)
		return
	}
	s.render(c, http.StatusOK, s.settingsPage(v, ""))
}

func (s *Server) settingsPage(v Settings, message string) page {
	return page{Page: "settings", Title: s.text("Settings"), Settings: v, Error: message,
		TokenPresent: os.Getenv("OCR_GITEA_TOKEN") != "", ModelKeyPresent: os.Getenv("OCR_LLM_API_KEY") != ""}
}

func (s *Server) saveSettings(c *gin.Context) {
	budget, _ := strconv.Atoi(c.PostForm("token_budget"))
	v := Settings{GiteaURL: c.PostForm("gitea_url"), GiteaUser: c.PostForm("gitea_user"),
		ModelEndpoint: c.PostForm("model_endpoint"), ModelName: c.PostForm("model_name"),
		ModelSource: c.PostForm("model_source"), ContextEndpoint: c.PostForm("context_endpoint"),
		Language: c.PostForm("language"), TokenBudget: budget, TokenBudgetEnabled: c.PostForm("token_budget_enabled") == "1", Version: version(c)}
	if err := v.Validate(); err != nil {
		s.render(c, http.StatusUnprocessableEntity, s.settingsPage(v, err.Error()))
		return
	}
	if err := s.store.SaveSettings(c.Request.Context(), v); err != nil {
		if errors.Is(err, ErrConflict) {
			s.render(c, http.StatusConflict, s.settingsPage(v, err.Error()))
		} else {
			s.fail(c, err)
		}
		return
	}
	c.Redirect(http.StatusSeeOther, "/settings?saved=1")
}
