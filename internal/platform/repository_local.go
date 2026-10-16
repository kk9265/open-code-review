// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package platform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func containsRepositoryPath(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func syncLocalRepository(ctx context.Context, dataDir string, id int64, source string, output io.Writer) (string, string, error) {
	if !filepath.IsAbs(source) {
		return "", "", errors.New("local source must be an absolute bare repository path")
	}
	source, err := filepath.EvalSymlinks(source)
	if err != nil {
		return "", "", errors.New("cannot resolve the local source repository")
	}
	root, err := managedDirectory(dataDir, "repositories")
	if err != nil {
		return "", "", err
	}
	if containsRepositoryPath(root, source) || containsRepositoryPath(source, root) {
		return "", "", errors.New("local source must be separate from the managed cache directory")
	}
	var bare bytes.Buffer
	if err := runManagedGitWithOutput(ctx, nil, &bare, "-c", "safe.directory="+source, "--git-dir="+source, "rev-parse", "--is-bare-repository"); err != nil || strings.TrimSpace(bare.String()) != "true" {
		return "", "", errors.New("local source must be a readable bare Git repository")
	}
	if _, err := os.Lstat(filepath.Join(source, "objects", "info", "alternates")); !errors.Is(err, os.ErrNotExist) {
		return "", "", errors.New("local sources with object alternates are not supported")
	}
	var head bytes.Buffer
	if err := runManagedGitWithOutput(ctx, nil, &head, "-c", "safe.directory="+source, "--git-dir="+source, "symbolic-ref", "HEAD"); err != nil {
		return "", "", errors.New("local source must have a default branch")
	}
	ref := strings.TrimSpace(head.String())
	if !strings.HasPrefix(ref, "refs/heads/") {
		return "", "", errors.New("local source HEAD must name a branch")
	}
	if err := runManagedGit(ctx, nil, "-c", "safe.directory="+source, "--git-dir="+source, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}"); err != nil {
		return "", "", errors.New("local source default branch has no valid commit")
	}
	target := filepath.Join(root, strconv.FormatInt(id, 10)+".git")
	info, err := os.Lstat(target)
	isNew := errors.Is(err, os.ErrNotExist)
	if err != nil && !isNew {
		return "", "", err
	}
	if !isNew {
		resolved, err := filepath.EvalSymlinks(target)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !samePath(target, resolved) {
			return "", "", errors.New("invalid managed cache path")
		}
	}
	work := target
	preserveWork := false
	if isNew {
		work, err = os.MkdirTemp(root, ".local-sync-")
		if err != nil {
			return "", "", err
		}
		defer func() {
			if !preserveWork {
				_ = os.RemoveAll(work)
			}
		}()
		fmt.Fprintln(output, "Copying local Git objects into an independent cache (no hardlinks).")
		if err := runManagedGitWithOutput(ctx, nil, output, "-c", "safe.directory="+source, "-c", "protocol.file.allow=always", "clone", "--bare", "--local", "--no-hardlinks", "--template=", "--", source, work); err != nil {
			return "", "", err
		}
		if err := runManagedGit(ctx, nil, "--git-dir="+work, "config", "--remove-section", "remote.origin"); err != nil {
			return "", "", err
		}
	}
	// Reconcile refs after copying; a concurrent push can change the source during clone.
	if err := runManagedGitWithOutput(ctx, nil, output, "-c", "safe.directory="+source, "-c", "protocol.file.allow=always", "--git-dir="+work, "fetch", "--progress", "--atomic", "--prune", "--no-recurse-submodules", "--no-write-fetch-head", "--", source, "+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*"); err != nil {
		return "", "", err
	}
	if err := runManagedGit(ctx, nil, "--git-dir="+work, "symbolic-ref", "HEAD", ref); err != nil {
		return "", "", err
	}
	if isNew {
		if err := finalizeLocalCache(ctx, work, target); err != nil {
			preserveWork = true
			return "", "", fmt.Errorf("cannot finalize cache; completed copy retained at %s: %w", work, err)
		}
	}
	return target, strings.TrimPrefix(ref, "refs/heads/"), nil
}

func finalizeLocalCache(ctx context.Context, work, target string) error {
	for attempt := 0; ; attempt++ {
		err := os.Rename(work, target)
		if err == nil || !errors.Is(err, os.ErrPermission) || attempt == 25 {
			return err
		}
		// Windows scanners can briefly hold a directory after Git has exited.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
