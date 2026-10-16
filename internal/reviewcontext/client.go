// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

// Package reviewcontext defines the optional, read-only context service boundary.
package reviewcontext

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Scope struct {
	RepositoryID int64  `json:"repository_id"`
	Revision     string `json:"revision"`
	Branch       string `json:"branch"`
}

type Session struct {
	ID           string   `json:"session_id"`
	Revision     string   `json:"revision"`
	Capabilities []string `json:"capabilities"`
	Notice       string   `json:"notice"`
}

type Client struct {
	endpoint string
	http     *http.Client
}

func ValidateEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return errors.New("Context endpoint must be a loopback HTTP URL.")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "http" || ip == nil || !ip.IsLoopback() || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("Context endpoint must be a loopback HTTP URL without a path or credentials.")
	}
	return nil
}

func New(endpoint string) (*Client, error) {
	if err := ValidateEndpoint(endpoint); err != nil {
		return nil, err
	}
	return &Client{endpoint: strings.TrimRight(endpoint, "/"), http: &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *Client) post(ctx context.Context, path string, input, output any) error {
	data, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, (256<<10)+1))
	if err != nil {
		return err
	}
	if len(body) > 256<<10 {
		return errors.New("Context response exceeds the size limit.")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Context service returned HTTP %d: %s", resp.StatusCode, string(body))
	}
	return json.Unmarshal(body, output)
}

func (c *Client) Open(ctx context.Context, scope Scope) (Session, error) {
	var s Session
	err := c.post(ctx, "/v1/open", scope, &s)
	if err == nil && (s.ID == "" || s.Revision != scope.Revision) {
		err = errors.New("Context service returned a different review revision.")
	}
	return s, err
}

func (c *Client) Query(ctx context.Context, s Session, capability, query string) (string, error) {
	var result struct {
		Revision string `json:"revision"`
		Text     string `json:"text"`
	}
	err := c.post(ctx, "/v1/query", map[string]any{"session_id": s.ID, "capability": capability, "query": query}, &result)
	if err == nil && result.Revision != s.Revision {
		return "", errors.New("Context response revision changed during review.")
	}
	return result.Text, err
}

func (c *Client) Close(ctx context.Context, s Session) error {
	var result map[string]any
	return c.post(ctx, "/v1/close", map[string]string{"session_id": s.ID}, &result)
}
