// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package platform

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func (s *Server) wikiPage(c *gin.Context) {
	c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; frame-src 'self'; img-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	view := "read"
	if c.Query("view") == "plan" {
		view = "plan"
	}
	s.render(c, http.StatusOK, page{Page: "wiki", Title: s.text("Wiki"), Query: view})
}

func (s *Server) wikiService(target, prefix string) gin.HandlerFunc {
	proxy := newWikiProxy(target, prefix)
	return func(c *gin.Context) {
		c.Writer.Header().Del("Content-Security-Policy")
		c.Writer.Header().Del("X-Frame-Options")
		proxy.ServeHTTP(c.Writer, c.Request)
	}
}

func newWikiProxy(target, prefix string) *httputil.ReverseProxy {
	upstream, _ := url.Parse(target)
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	director := proxy.Director
	proxy.Director = func(r *http.Request) {
		r.URL.Path = strings.TrimPrefix(r.URL.Path, prefix)
		r.URL.RawPath = ""
		if r.URL.Path == "" {
			r.URL.Path = "/"
		}
		director(r)
		r.Host = upstream.Host
		r.Header.Del("Cookie")
		r.Header.Del("Authorization")
		r.Header.Set("Accept-Encoding", "identity")
	}
	proxy.Transport = &http.Transport{Proxy: nil, ResponseHeaderTimeout: 15 * time.Second}
	proxy.ModifyResponse = func(r *http.Response) error {
		r.Header.Del("X-Frame-Options")
		r.Header.Del("Set-Cookie")
		csp := strings.ReplaceAll(r.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'", "frame-ancestors 'self'")
		r.Header.Set("Content-Security-Policy", csp)
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "text/html") {
			return nil
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
		r.Body.Close()
		if err != nil {
			return err
		}
		rewrite := strings.NewReplacer(
			`href="/`, `href="`+prefix+`/`, `src="/`, `src="`+prefix+`/`,
			`<html `, `<html data-wiki-base="`+prefix+`" `,
			`href="http://127.0.0.1:5490/planning"`, `target="_top" href="/wiki?view=plan"`,
			`href="http://127.0.0.1:5491/"`, `target="_top" href="/wiki?view=read"`,
			`href="http://127.0.0.1:5484/reviews"`, `target="_top" href="/reviews"`,
		)
		body = []byte(rewrite.Replace(string(body)))
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		r.Header.Set("Content-Length", strconv.Itoa(len(body)))
		return nil
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "Wiki service is unavailable. Please retry after starting the local Wiki service.", http.StatusServiceUnavailable)
	}
	return proxy
}
