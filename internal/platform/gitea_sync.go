// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package platform

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var errSyncInterrupted = errors.New("Repository sync was interrupted. Retry to continue.")

func (s *Server) RunRepositorySync(ctx context.Context) {
	if err := s.store.interruptGiteaSync(ctx); err != nil {
		s.log.Error("recover repository sync", "error", err)
		return
	}
	for ctx.Err() == nil {
		s.syncMu.Lock()
		if err := s.store.queueTrackedRepositories(ctx); err != nil {
			s.log.Error("schedule tracked repositories", "error", err)
		}
		source, err := s.store.nextGiteaSync(ctx)
		if err != nil {
			s.log.Error("claim repository sync", "error", err)
		}
		if source == nil || err != nil {
			s.syncMu.Unlock()
			select {
			case <-ctx.Done():
				return
			case <-s.syncWake:
			case <-time.After(30 * time.Second):
			}
			continue
		}
		syncCtx, cancel := context.WithTimeout(ctx, 2*time.Hour)
		path, remoteURL, remote, syncErr := s.syncGitea(syncCtx, source)
		cancel()
		finishCtx, finishCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if ctx.Err() != nil {
			syncErr = errSyncInterrupted
		}
		if err := s.store.finishGiteaSync(finishCtx, source.RepositoryID, path, remote.DefaultBranch, remote.Owner.Login+"/"+remote.Name, remoteURL, syncErr); err != nil {
			s.log.Error("save repository sync result", "repository_id", source.RepositoryID, "error", err)
		}
		finishCancel()
		trackCtx, trackCancel := context.WithTimeout(ctx, 2*time.Minute)
		s.trackingResult(trackCtx, source.RepositoryID, path, syncErr)
		trackCancel()
		s.syncMu.Unlock()
	}
}

func (s *Server) wakeRepositorySync() {
	select {
	case s.syncWake <- struct{}{}:
	default:
	}
}

func (s *Server) syncGitea(ctx context.Context, source *GiteaSource) (string, string, GiteaRepository, error) {
	return s.configuredSync(ctx, source, nil)
}

func (s *Server) syncGiteaWithProgress(ctx context.Context, source *GiteaSource, progress io.Writer) (string, string, GiteaRepository, error) {
	var remote GiteaRepository
	settings, err := s.store.Settings(ctx)
	if err != nil {
		return "", "", remote, errors.New("Cannot read the Gitea settings.")
	}
	if settings.GiteaURL != source.ServerURL {
		return "", "", remote, errors.New("This repository belongs to a different Gitea server. Restore its server address in Settings before syncing.")
	}
	client, err := newGiteaClient(settings)
	if err != nil {
		return "", "", remote, err
	}
	user, err := client.identity(ctx)
	if err != nil {
		return "", "", remote, err
	}
	remote, err = client.repository(ctx, source.RemoteID)
	if err != nil {
		return "", "", remote, err
	}
	if remote.Empty {
		return "", "", remote, errors.New("This repository has no commits yet.")
	}
	if _, err := exec.LookPath("git"); err != nil {
		return "", "", remote, errors.New("Git is not installed or is not available on PATH.")
	}
	root, err := managedDirectory(s.config.DataDir, "repositories")
	if err != nil {
		return "", "", remote, err
	}
	path := filepath.Join(root, strconv.FormatInt(source.RepositoryID, 10)+".git")
	remoteURL := client.repositoryURL(remote)
	info, err := os.Lstat(path)
	isNew := errors.Is(err, os.ErrNotExist)
	if err != nil && !isNew {
		return "", "", remote, errors.New("Cannot access the local repository cache.")
	}
	if !isNew {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !samePath(path, resolved) {
			return "", "", remote, errors.New("The repository cache path is not a managed directory.")
		}
	}
	work := path
	if isNew {
		work, err = os.MkdirTemp(root, ".sync-")
		if err != nil {
			return "", "", remote, errors.New("Cannot create the local repository cache.")
		}
		// Only this attempt's temporary directory may be removed after failure.
		defer os.RemoveAll(work)
		if err := runManagedGit(ctx, nil, "init", "--bare", "--template=", "--", work); err != nil {
			return "", "", remote, err
		}
	}
	auth := "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(user.Login+":"+client.token))
	env := []string{"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http." + client.base + "/.extraHeader", "GIT_CONFIG_VALUE_0=" + auth}
	fetchArgs := []string{"--git-dir=" + work, "fetch", "--atomic", "--prune", "--no-recurse-submodules", "--no-write-fetch-head"}
	if progress != nil {
		fetchArgs = append(fetchArgs, "--progress")
	}
	fetchArgs = append(fetchArgs, "--", remoteURL+".git", "+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*")
	if err := runManagedGitWithOutput(ctx, env, progress, fetchArgs...); err != nil {
		return "", "", remote, err
	}
	if remote.DefaultBranch != "" {
		if err := runManagedGit(ctx, nil, "--git-dir="+work, "symbolic-ref", "HEAD", "refs/heads/"+remote.DefaultBranch); err != nil {
			return "", "", remote, err
		}
	}
	if isNew {
		if err := os.Rename(work, path); err != nil {
			return "", "", remote, errors.New("Cannot finalize the local repository cache.")
		}
	}
	return path, remoteURL, remote, nil
}

func managedDirectory(dataDir, name string) (string, error) {
	base, err := filepath.EvalSymlinks(dataDir)
	if err != nil {
		return "", errors.New("Cannot access the data directory.")
	}
	root := filepath.Join(base, name)
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", errors.New("Cannot create the repository cache directory.")
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil || !samePath(root, resolved) {
		return "", errors.New("The repository cache path is not a managed directory.")
	}
	return root, nil
}

func runManagedGit(ctx context.Context, extraEnv []string, args ...string) error {
	return runManagedGitWithOutput(ctx, extraEnv, nil, args...)
}

func runManagedGitWithOutput(ctx context.Context, extraEnv []string, output io.Writer, args ...string) error {
	options := []string{"-c", "credential.helper=", "-c", "core.hooksPath=" + os.DevNull, "-c", "http.followRedirects=false", "-c", "http.lowSpeedLimit=1", "-c", "http.lowSpeedTime=60", "-c", "protocol.allow=never", "-c", "protocol.http.allow=always", "-c", "protocol.https.allow=always", "-c", "maintenance.auto=false", "-c", "gc.auto=0"}
	cmd := exec.CommandContext(ctx, "git", append(options, args...)...)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		key = strings.ToUpper(key)
		if strings.HasPrefix(key, "GIT_") || strings.HasPrefix(key, "GCM_") || key == "OCR_GITEA_TOKEN" || key == "OCR_LLM_API_KEY" {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_SYSTEM="+os.DevNull, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=Never", "GIT_LFS_SKIP_SMUDGE=1")
	cmd.Env = append(cmd.Env, extraEnv...)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if output != nil {
		cmd.Stdout, cmd.Stderr = output, output
	}
	cmd.WaitDelay = 5 * time.Second
	configureGitProcess(cmd)
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return errors.New("Repository sync timed out or was interrupted. Retry to continue.")
		}
		return errors.New("Git could not fetch the repository. Check repository access, token permissions, network, and free disk space.")
	}
	return nil
}
