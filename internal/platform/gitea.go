// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package platform

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

var errGiteaToken = errors.New("Configure OCR_GITEA_TOKEN and restart the workbench first.")

type giteaClient struct {
	base, token, user string
	http              *http.Client
}

type GiteaUser struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}

type GiteaRepository struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	FullName      string    `json:"full_name"`
	Description   string    `json:"description"`
	Owner         GiteaUser `json:"owner"`
	DefaultBranch string    `json:"default_branch"`
	Private       bool      `json:"private"`
	Empty         bool      `json:"empty"`
	Archived      bool      `json:"archived"`
	LocalID       int64     `json:"-"`
}

var giteaHTTP = &http.Client{
	Timeout:       15 * time.Second,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
}

func newGiteaClient(settings Settings) (*giteaClient, error) {
	if err := validateURL(settings.GiteaURL); err != nil {
		return nil, err
	}
	token := strings.TrimSpace(os.Getenv("OCR_GITEA_TOKEN"))
	if token == "" {
		return nil, errGiteaToken
	}
	return &giteaClient{base: strings.TrimRight(settings.GiteaURL, "/"), user: settings.GiteaUser, token: token, http: giteaHTTP}, nil
}

func (g *giteaClient) get(ctx context.Context, path string, query url.Values, target any) (bool, error) {
	address := g.base + "/api/v1" + path
	if len(query) != 0 {
		address += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return false, errors.New("Invalid Gitea address.")
	}
	req.Header.Set("Authorization", "token "+g.token)
	req.Header.Set("Accept", "application/json")
	res, err := g.http.Do(req)
	if err != nil {
		return false, errors.New("Cannot reach Gitea. Check the address and network connection.")
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return false, errors.New("Gitea rejected the token. Check whether it has expired or been revoked.")
	case http.StatusForbidden:
		return false, errors.New("Gitea denied access. The token needs read:user and read:repository permissions.")
	case http.StatusNotFound:
		return false, errors.New("The Gitea repository does not exist or is not accessible to this account.")
	case http.StatusTooManyRequests:
		return false, errors.New("Gitea is rate limiting requests. Please try again later.")
	default:
		return false, errors.New("Gitea returned an unexpected response. Check the server address and availability.")
	}
	const limit = 4 << 20
	body, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil || len(body) > limit || json.Unmarshal(body, target) != nil {
		return false, errors.New("Gitea returned an invalid or oversized response.")
	}
	return strings.Contains(res.Header.Get("Link"), `rel="next"`), nil
}

func (g *giteaClient) identity(ctx context.Context) (GiteaUser, error) {
	var user GiteaUser
	_, err := g.get(ctx, "/user", nil, &user)
	if err != nil {
		return user, err
	}
	if user.ID < 1 || user.Login == "" {
		return user, errors.New("Gitea returned an invalid account.")
	}
	if g.user != "" && !strings.EqualFold(user.Login, g.user) {
		return user, errors.New("The token belongs to a different account. Check the username in Settings.")
	}
	return user, nil
}

func (g *giteaClient) repositories(ctx context.Context, query string, page int) ([]GiteaRepository, bool, error) {
	var result struct {
		OK   bool              `json:"ok"`
		Data []GiteaRepository `json:"data"`
	}
	next, err := g.get(ctx, "/repos/search", url.Values{
		"q": {query}, "private": {"true"}, "limit": {"25"}, "page": {strconv.Itoa(page)}, "sort": {"updated"}, "order": {"desc"},
	}, &result)
	if err != nil {
		return nil, false, err
	}
	if !result.OK {
		return nil, false, errors.New("Gitea could not list repositories.")
	}
	return result.Data, next, nil
}

func (g *giteaClient) repository(ctx context.Context, id int64) (GiteaRepository, error) {
	var repo GiteaRepository
	_, err := g.get(ctx, "/repositories/"+strconv.FormatInt(id, 10), nil, &repo)
	if err != nil {
		return repo, err
	}
	if repo.ID != id || !giteaSegment(repo.Owner.Login) || !giteaSegment(repo.Name) {
		return repo, errors.New("Gitea returned an invalid repository.")
	}
	return repo, nil
}

func giteaSegment(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, "/\\\x00\r\n")
}

func (g *giteaClient) repositoryURL(repo GiteaRepository) string {
	return g.base + "/" + url.PathEscape(repo.Owner.Login) + "/" + url.PathEscape(repo.Name)
}
