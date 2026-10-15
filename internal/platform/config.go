// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package platform

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Addr    string
	DataDir string
}

func DefaultConfig() Config {
	return Config{Addr: envOr("OCR_WEB_ADDR", "127.0.0.1:5484"), DataDir: envOr("OCR_WEB_DATA_DIR", ".ocr-data")}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func (c *Config) Validate() error {
	host, port, err := net.SplitHostPort(c.Addr)
	if err != nil {
		return fmt.Errorf("invalid listen address: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("listen address must use a loopback IP, such as 127.0.0.1 or ::1")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	if strings.TrimSpace(c.DataDir) == "" {
		return errors.New("data directory is required")
	}
	c.DataDir, err = filepath.Abs(c.DataDir)
	return err
}

type Settings struct {
	GiteaURL      string `json:"gitea_url"`
	GiteaUser     string `json:"gitea_user"`
	ModelEndpoint string `json:"model_endpoint"`
	ModelName     string `json:"model_name"`
	Language      string `json:"language"`
	TokenBudget   int    `json:"token_budget"`
	Version       int64  `json:"-"`
}

func defaultSettings() Settings {
	return Settings{GiteaURL: "http://10.30.142.20:3000", GiteaUser: "kk9265", Language: "zh-CN", TokenBudget: 50000}
}

func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("use an HTTP or HTTPS URL without credentials, query parameters, or fragments")
	}
	return nil
}

func (s *Settings) Validate() error {
	s.GiteaURL = strings.TrimRight(strings.TrimSpace(s.GiteaURL), "/")
	s.GiteaUser = strings.TrimSpace(s.GiteaUser)
	s.ModelEndpoint = strings.TrimRight(strings.TrimSpace(s.ModelEndpoint), "/")
	s.ModelName = strings.TrimSpace(s.ModelName)
	if err := validateURL(s.GiteaURL); err != nil {
		return fmt.Errorf("Gitea URL: %w", err)
	}
	if len(s.GiteaURL) > 2048 || len(s.ModelEndpoint) > 2048 || len(s.GiteaUser) > 100 || len(s.ModelName) > 200 {
		return errors.New("a configuration field is too long")
	}
	if s.ModelEndpoint != "" {
		if err := validateURL(s.ModelEndpoint); err != nil {
			return fmt.Errorf("model endpoint: %w", err)
		}
	}
	if s.Language != "zh-CN" && s.Language != "en" {
		return errors.New("review language must be zh-CN or en")
	}
	if s.TokenBudget < 1000 || s.TokenBudget > 1000000 {
		return errors.New("token budget must be between 1000 and 1000000")
	}
	return nil
}
