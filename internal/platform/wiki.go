// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	wikiassets "github.com/alibaba/open-code-review/integrations/wiki-preview"
	"github.com/gin-gonic/gin"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

var wikiSlug = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
var wikiCommit = regexp.MustCompile(`^(?:[a-f0-9]{40}|[a-f0-9]{64})$`)

type wikiDocument struct {
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Parent    string `json:"parent_slug"`
	Type      string `json:"page_type"`
	Markdown  string `json:"markdown"`
	Hash      string `json:"markdown_sha256"`
	Model     string `json:"generation_model"`
	Functions []struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Signature string `json:"signature"`
		Summary   string `json:"summary"`
	} `json:"function_index"`
	Sections []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Summary string `json:"summary"`
	} `json:"section_index"`
	Sources []struct {
		File string `json:"file_path"`
	} `json:"source_refs"`
}
type wikiManifest struct {
	Mode              string         `json:"mode"`
	ReviewStatus      string         `json:"review_status"`
	MarkdownDirectory string         `json:"markdown_directory"`
	Repository        string         `json:"repository"`
	Scope             string         `json:"scope"`
	Commit            string         `json:"commit"`
	Pages             []wikiDocument `json:"pages"`
}

func (s *Server) wikiPage(c *gin.Context) {
	scope := c.Query("scope")
	if scope == "" {
		scope = "LoginServer"
	}
	if scope != "LoginServer" && scope != "AccountServer" {
		c.Status(404)
		return
	}
	c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; frame-src 'self'; img-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	view := "read"
	if c.Query("view") == "plan" {
		view = "plan"
	}
	s.render(c, http.StatusOK, page{Page: "wiki", Title: s.text("Wiki"), Query: view, WikiScope: scope, WikiPrefix: "/wiki/scopes/" + scope})
}

func wikiScope(c *gin.Context) (string, string, bool) {
	scope := c.Param("scope")
	if scope == "" {
		return "loginserver", "/wiki", true
	}
	if scope != "LoginServer" && scope != "AccountServer" {
		c.Status(404)
		return "", "", false
	}
	return strings.ToLower(scope), "/wiki/scopes/" + scope, true
}

func wikiHeaders(c *gin.Context) {
	c.Header("X-Frame-Options", "SAMEORIGIN")
	c.Header("Content-Security-Policy", "default-src 'none'; script-src 'self'; connect-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self'; base-uri 'none'; frame-ancestors 'self'")
}

func wikiRead(root, name string) ([]byte, error) {
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return dir.ReadFile(name)
}

func wikiAsset(c *gin.Context, name, prefix string) {
	body, err := wikiassets.Files.ReadFile("ui/" + name)
	if err != nil {
		c.Status(404)
		return
	}
	kind := "text/javascript; charset=utf-8"
	switch filepath.Ext(name) {
	case ".html":
		kind = "text/html; charset=utf-8"
		body = []byte(strings.NewReplacer(`href="/`, `href="`+prefix+`/`, `src="/`, `src="`+prefix+`/`, `<html `, `<html data-wiki-base="`+prefix+`" `,
			`href="http://127.0.0.1:5491/"`, `target="_top" href="/wiki?view=read"`, `href="http://127.0.0.1:5484/reviews"`, `target="_top" href="/reviews"`).Replace(string(body)))
		if scope := c.Param("scope"); scope != "" {
			body = []byte(strings.NewReplacer("/wiki?view=read", "/wiki?view=read&amp;scope="+scope, "LoginServer Wiki", scope+" Wiki").Replace(string(body)))
		}
	case ".css":
		kind = "text/css; charset=utf-8"
	case ".json":
		kind = "application/json; charset=utf-8"
	}
	c.Data(200, kind, body)
}

func (s *Server) wikiReader(c *gin.Context) {
	scope, base, ok := wikiScope(c)
	if !ok {
		return
	}
	wikiHeaders(c)
	path := c.Param("path")
	static := map[string]string{"/style.css": "generated-style.css", "/wiki-reader.css": "wiki-reader.css", "/wiki-reader.js": "wiki-reader.js", "/wiki-diagrams.js": "wiki-diagrams.js", "/i18n.json": "i18n/zh-CN.json"}
	if name, ok := static[path]; ok {
		wikiAsset(c, name, base+"/read")
		return
	}
	if strings.HasPrefix(path, "/vendor/mermaid/") {
		name := strings.TrimPrefix(path, "/vendor/mermaid/")
		if ext := filepath.Ext(name); ext != ".js" && ext != ".mjs" {
			c.Status(404)
			return
		}
		body, err := wikiRead(filepath.Join(s.config.DataDir, "wiki-mermaid/node_modules/mermaid/dist"), name)
		if err != nil {
			c.Status(404)
			return
		}
		c.Data(200, "text/javascript; charset=utf-8", body)
		return
	}
	root := filepath.Join(s.config.DataDir, "wiki-"+scope+"-generated")
	raw, err := wikiRead(root, "manifest.json")
	var manifest wikiManifest
	if err != nil || json.Unmarshal(raw, &manifest) != nil || !wikiCommit.MatchString(manifest.Commit) {
		s.wikiUnavailable(c)
		return
	}
	for _, p := range manifest.Pages {
		if !wikiSlug.MatchString(p.Slug) {
			s.wikiUnavailable(c)
			return
		}
	}
	if path == "/api/catalog" {
		pages := make([]string, 0, len(manifest.Pages))
		for _, p := range manifest.Pages {
			pages = append(pages, p.Slug)
		}
		c.JSON(200, gin.H{"repository": manifest.Repository, "scope": manifest.Scope, "commit": manifest.Commit, "pages": pages, "mode": manifest.Mode})
		return
	}
	copyBytes, _ := wikiassets.Files.ReadFile("ui/i18n/zh-CN.json")
	labels := map[string]string{}
	_ = json.Unmarshal(copyBytes, &labels)
	t := func(key string) string {
		if v, ok := labels[key]; ok {
			return html.EscapeString(v)
		}
		return html.EscapeString(key)
	}
	var content string
	selected := ""
	if path == "/" {
		var progress struct {
			Model     string         `json:"model"`
			Completed int            `json:"completed_pages"`
			Planned   int            `json:"planned_pages"`
			Models    map[string]int `json:"generated_models"`
		}
		raw, err = wikiRead(root, "progress.json")
		if err != nil || json.Unmarshal(raw, &progress) != nil {
			s.wikiUnavailable(c)
			return
		}
		content = "<h1>" + html.EscapeString(manifest.Scope) + " Wiki</h1><p>AI Mana / " + html.EscapeString(progress.Model) + " · " + t("generatedOnlyByTool") + "</p>"
		models := []string{}
		for model, count := range progress.Models {
			models = append(models, fmt.Sprintf("%s: %d", model, count))
		}
		sort.Strings(models)
		choose := "readerChoosePage"
		if manifest.Mode == "function-guide" {
			choose = "guideChoose"
		}
		content += "<p>" + html.EscapeString(strings.Join(models, " · ")) + "</p>" + fmt.Sprintf("<p>%s: %d / %d</p><p>%s</p>", t("generatedProgress"), progress.Completed, progress.Planned, t(choose))
		if len(manifest.Pages) == 0 {
			content = "<h1>" + html.EscapeString(manifest.Scope) + " Wiki</h1><p>" + t("guideEmpty") + "</p>"
		}
	} else if strings.HasPrefix(path, "/source/") {
		sourcePath := strings.TrimPrefix(path, "/source/")
		allowed := false
		for _, p := range manifest.Pages {
			for _, ref := range p.Sources {
				if ref.File == sourcePath {
					allowed = true
				}
			}
		}
		if !allowed {
			c.Status(404)
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		raw, err = exec.CommandContext(ctx, "git", "-C", filepath.Join(s.config.DataDir, "wiki-zgame-plan/zgame"), "show", manifest.Commit+":"+sourcePath).Output()
		if err != nil {
			s.wikiUnavailable(c)
			return
		}
		back := "/wiki/read/"
		for _, p := range manifest.Pages {
			if p.Slug == c.Query("page") {
				back += "page/" + p.Slug
				for _, function := range p.Functions {
					if function.ID == c.Query("function") && wikiSlug.MatchString(function.ID) {
						back += "#" + function.ID
						break
					}
				}
				for _, section := range p.Sections {
					if section.ID == c.Query("function") && wikiSlug.MatchString(section.ID) {
						back += "#" + section.ID
						break
					}
				}
			}
		}
		content = `<p><a href="` + back + `">` + t("readerBack") + `</a></p><h1>` + html.EscapeString(sourcePath) + "</h1><p><code>" + manifest.Commit + "</code></p><pre>"
		for i, line := range strings.Split(strings.TrimSuffix(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n"), "\n") {
			content += fmt.Sprintf(`<span id="L%d">%d  %s</span>`+"\n", i+1, i+1, html.EscapeString(line))
		}
		content += "</pre>"
	} else {
		found := false
		for _, p := range manifest.Pages {
			if path != "/page/"+p.Slug {
				continue
			}
			found = true
			selected = p.Slug
			directory := manifest.MarkdownDirectory
			if directory == "" {
				directory = "markdown"
			}
			raw, err = wikiRead(root, directory+"/"+p.Slug+".md")
			if err != nil || !bytes.Equal(raw, []byte(p.Markdown)) || fmt.Sprintf("%x", sha256.Sum256(raw)) != p.Hash {
				s.wikiUnavailable(c)
				return
			}
			md := goldmark.New(goldmark.WithExtensions(extension.GFM), goldmark.WithParserOptions(parser.WithAttribute()))
			doc := md.Parser().Parse(text.NewReader(raw))
			_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
				if link, ok := node.(*ast.Link); ok && entering {
					// Legacy manifests include Windows file URLs with unescaped backslashes.
					if _, tail, ok := strings.Cut(string(link.Destination), "/blob/"+manifest.Commit+"/"); ok {
						if u, e := url.Parse("/" + tail); e == nil {
							link.Destination = []byte((&url.URL{Path: "/wiki/read/source" + u.Path, RawQuery: url.Values{"page": {p.Slug}}.Encode(), Fragment: u.Fragment}).String())
						}
					}
				}
				return ast.WalkContinue, nil
			})
			var out bytes.Buffer
			if md.Renderer().Render(&out, raw, doc) != nil {
				s.wikiUnavailable(c)
				return
			}
			content = "<p class=\"muted\">AI Mana / " + html.EscapeString(p.Model) + "</p>" + out.String()
			break
		}
		if !found {
			c.Status(404)
			return
		}
	}
	if path == "/" || selected != "" {
		content = wikiLayout(manifest, selected, content, t)
	}
	title := html.EscapeString(manifest.Scope) + " Wiki"
	body := `<!DOCTYPE html><html lang="zh-CN" data-wiki-base="/wiki/read"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>` + title + `</title><link rel="stylesheet" href="/wiki/read/style.css"><link rel="stylesheet" href="/wiki/read/wiki-reader.css"><script type="module" src="/wiki/read/wiki-reader.js"></script><header><strong>` + title + `</strong><a target="_top" href="/wiki?view=plan">` + t("generatedPlanning") + `</a><a target="_top" href="/reviews">` + t("backReview") + `</a></header><main>` + content + `</main></html>`
	body = strings.ReplaceAll(body, "/wiki/read", base+"/read")
	if c.Param("scope") != "" {
		body = strings.ReplaceAll(body, "/wiki?view=plan", "/wiki?view=plan&amp;scope="+c.Param("scope"))
	}
	c.Data(200, "text/html; charset=utf-8", []byte(body))
}

func (s *Server) wikiUnavailable(c *gin.Context) {
	c.String(503, s.text("Wiki output is not ready or failed integrity verification."))
}

func wikiLayout(m wikiManifest, selected, content string, t func(string) string) string {
	sort.SliceStable(m.Pages, func(i, j int) bool { return strings.ToLower(m.Pages[i].Title) < strings.ToLower(m.Pages[j].Title) })
	link := func(p wikiDocument) string {
		search := p.Title
		for _, r := range p.Sources {
			search += " " + r.File
		}
		for _, function := range p.Functions {
			search += " " + function.Signature + " " + function.Summary
		}
		current := ""
		if p.Slug == selected {
			current = ` aria-current="page"`
		}
		label := p.Title
		if p.Type == "function" && len(p.Functions) == 1 {
			name := p.Functions[0].Name
			duplicates := 0
			for _, sibling := range m.Pages {
				if sibling.Parent == p.Parent && len(sibling.Functions) == 1 && sibling.Functions[0].Name == name {
					duplicates++
				}
			}
			if duplicates == 1 {
				parts := strings.Split(name, "::")
				label = parts[len(parts)-1]
			}
		}
		count := ""
		if p.Type == "function-group" {
			children := 0
			for _, child := range m.Pages {
				if child.Parent == p.Slug {
					children++
				}
			}
			count = fmt.Sprintf(` <span class="reader-count">%d</span>`, children)
		}
		return `<a class="reader-page" title="` + html.EscapeString(p.Title) + `" data-search="` + html.EscapeString(search) + `" href="/wiki/read/page/` + p.Slug + `"` + current + `><span class="reader-label">` + html.EscapeString(label) + `</span>` + count + `</a>`
	}
	searchLabel, filesLabel := "readerSearch", "files"
	featureButton := `<button type="button" data-reader-view="features">` + t("features") + `</button>`
	if m.Mode == "function-guide" {
		searchLabel, filesLabel = "guideSearch", "guideBusinessIndex"
		featureButton = `<button type="button" data-reader-view="functions">` + t("guideFunctions") + `</button>`
	}
	nav := `<aside class="reader-sidebar"><p class="muted">` + html.EscapeString(m.Repository) + " · " + m.Commit[:12] + `</p><label for="reader-search">` + t(searchLabel) + `</label><input id="reader-search" type="search"><div class="reader-tabs"><button type="button" data-reader-view="files">` + t(filesLabel) + `</button>` + featureButton + `</div><nav id="reader-files">`
	navigation := func(functions bool) string {
		var output strings.Builder
		for _, p := range m.Pages {
			isFunctionGroup := p.Type == "function-group" || p.Type == "function-index"
			if functions != isFunctionGroup || (p.Type != "file" && p.Type != "class" && p.Type != "module" && !isFunctionGroup) {
				continue
			}
			children := ""
			opened := ""
			for _, section := range p.Sections {
				if len(p.Sections) == 1 && section.Name == p.Title {
					continue
				}
				if !wikiSlug.MatchString(section.ID) {
					continue
				}
				children += `<a class="reader-page reader-function" data-search="` + html.EscapeString(section.Name+" "+section.Summary) + `" href="/wiki/read/page/` + p.Slug + `#` + section.ID + `">` + html.EscapeString(section.Name) + `</a>`
			}
			for _, function := range p.Functions {
				if !wikiSlug.MatchString(function.ID) {
					continue
				}
				search := function.Signature + " " + function.Summary
				children += `<a class="reader-page reader-function" data-search="` + html.EscapeString(search) + `" href="/wiki/read/page/` + p.Slug + `#` + function.ID + `">` + html.EscapeString(function.Name) + `</a>`
			}
			if p.Slug == selected {
				opened = " open"
			}
			for _, child := range m.Pages {
				if child.Parent == p.Slug {
					children += link(child)
					if child.Slug == selected {
						opened = " open"
					}
				}
			}
			if children == "" {
				output.WriteString(`<div class="reader-file">` + link(p) + `</div>`)
			} else {
				output.WriteString(`<details class="reader-file" data-slug="` + p.Slug + `"` + opened + `><summary>` + link(p) + `</summary><div class="reader-children">` + children + `</div></details>`)
			}
		}
		return output.String()
	}
	nav += navigation(false) + `</nav><nav id="reader-functions" hidden>` + navigation(true)
	nav += `</nav><nav id="reader-features" hidden>`
	for _, p := range m.Pages {
		if p.Type == "feature" {
			nav += link(p)
		}
	}
	nav += `</nav><p id="reader-empty" hidden>` + t("noMatches") + `</p></aside>`
	crumbs := `<a href="/wiki/read/">` + t("generatedCatalog") + `</a>`
	for _, p := range m.Pages {
		if p.Slug != selected {
			continue
		}
		kind := "files"
		if p.Type == "class" {
			kind = "guideClasses"
		}
		if p.Type == "module" {
			kind = "guideBusinessIndex"
		}
		if p.Type == "function-index" {
			kind = "guideFunctionIndex"
		}
		if p.Type == "function-group" || p.Type == "function" {
			kind = "guideFunctions"
		}
		if p.Type == "feature" {
			kind = "features"
		}
		crumbs += " / " + t(kind)
		for _, parent := range m.Pages {
			if parent.Slug == p.Parent {
				crumbs += ` / <a href="/wiki/read/page/` + parent.Slug + `">` + html.EscapeString(parent.Title) + `</a>`
			}
		}
		crumbs += " / <span>" + html.EscapeString(p.Title) + "</span>"
	}
	if m.ReviewStatus == "awaiting_user_review" {
		label := "guidePilot"
		for _, document := range m.Pages {
			if document.Type == "module" || document.Type == "function-index" {
				label = "guideScopeReview"
				break
			}
		}
		content = `<p class="guide-pilot">` + t(label) + `</p>` + content
	}
	return `<div class="reader-layout" data-mode="` + html.EscapeString(m.Mode) + `" data-scope="` + html.EscapeString(m.Scope) + `" data-repository="` + html.EscapeString(m.Repository) + `" data-commit="` + m.Commit + `">` + nav + `<article class="reader-article"><nav class="reader-breadcrumbs" aria-label="` + t("readerBreadcrumbs") + `">` + crumbs + `</nav>` + content + `</article></div>`
}
