// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type ticketSnapshot struct {
	Path, Base, Commit, Final, Patch string
	Sources                          []string
}

// Removing only selected patches from the final tree preserves intervening tickets on both sides.
func (s *Server) prepareTicketSnapshot(ctx context.Context, j *ReviewJob) (*ticketSnapshot, error) {
	if j.snapshot != nil {
		return j.snapshot, nil
	}
	if len(j.Commits) == 0 {
		return nil, errors.New("This ticket has no commits to review.")
	}
	source, err := s.cachePath(j.RepositoryID)
	if err != nil {
		return nil, err
	}
	root := filepath.Join(s.config.DataDir, "review-inputs", strconv.FormatInt(j.ID, 10))
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	path, err := os.MkdirTemp(root, "snapshot-")
	if err != nil {
		return nil, err
	}
	run := func(input []byte, args ...string) (string, error) {
		out := &boundedPatch{limit: 2 << 20}
		err := runManagedGitWithInputOutput(ctx, nil, bytes.NewReader(input), out, append([]string{"--git-dir=" + path}, args...)...)
		if out.exceeded {
			return "", errors.New("The unified ticket input exceeds the size limit. No partial input was sent.")
		}
		return strings.TrimSpace(out.String()), err
	}
	format := "sha1"
	if len(j.Commits[len(j.Commits)-1].SHA) == 64 {
		format = "sha256"
	}
	if _, err := run(nil, "init", "--bare", "--quiet", "--object-format="+format); err != nil {
		return nil, err
	}
	objects, err := filepath.Abs(filepath.Join(source, "objects"))
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(path, "objects", "info", "alternates"), []byte(filepath.ToSlash(objects)+"\n"), 0600); err != nil {
		return nil, err
	}
	snapshot := &ticketSnapshot{Path: path, Final: j.Commits[len(j.Commits)-1].SHA}
	var previous string
	for _, c := range j.Commits {
		if !validReviewSHA(c.SHA) {
			return nil, errors.New("Invalid selected commit SHA.")
		}
		actual, err := run(nil, "show", "-s", "--format=%P", c.SHA, "--")
		if err != nil || actual != strings.TrimSpace(c.Parents) {
			return nil, errors.New("Selected commit parent identity mismatch.")
		}
		if previous != "" {
			if _, err := run(nil, "merge-base", "--is-ancestor", previous, c.SHA); err != nil {
				return nil, errors.New("Selected commits are not in ancestry order.")
			}
		}
		previous = c.SHA
		snapshot.Sources = append(snapshot.Sources, c.SHA)
	}
	if _, err := run(nil, "read-tree", snapshot.Final); err != nil {
		return nil, err
	}
	for i := len(j.Commits) - 1; i >= 0; i-- {
		c := j.Commits[i]
		var patch bytes.Buffer
		args := []string{"--git-dir=" + source, "diff-tree", "--root", "--no-commit-id", "-r", "-p", "--binary", "--full-index", "--no-renames", "--no-ext-diff", "--no-textconv"}
		if parents := strings.Fields(c.Parents); len(parents) > 0 {
			args = append(args, parents[0])
		}
		args = append(args, c.SHA, "--")
		bounded := &boundedPatch{limit: 2 << 20}
		if err := runManagedGitWithOutput(ctx, nil, bounded, args...); err != nil {
			return nil, fmt.Errorf("Cannot extract selected patch %s: %w", c.SHA, err)
		}
		patch.Write(bounded.Bytes())
		if patch.Len() == 0 {
			continue
		}
		if _, err := run(patch.Bytes(), "apply", "--cached", "--reverse", "--3way", "--whitespace=nowarn", "-"); err != nil {
			return nil, fmt.Errorf("Cannot safely separate ticket patch %s from intervening changes; review was not started", c.SHA)
		}
	}
	tree, err := run(nil, "write-tree")
	if err != nil {
		return nil, err
	}
	commitArgs := []string{"-c", "user.name=Review input", "-c", "user.email=ocr-input@localhost", "commit-tree"}
	snapshot.Base, err = run([]byte("Ticket input baseline\n"), append(commitArgs, tree)...)
	if err != nil {
		return nil, err
	}
	finalTree, err := run(nil, "rev-parse", snapshot.Final+"^{tree}")
	if err != nil {
		return nil, err
	}
	snapshot.Commit, err = run([]byte("Unified ticket changes\n"), append(commitArgs, finalTree, "-p", snapshot.Base)...)
	if err != nil {
		return nil, err
	}
	var patch boundedPatch
	patch.limit = 2 << 20
	if err := runManagedGitWithOutput(ctx, nil, &patch, "--git-dir="+path, "diff", "--no-ext-diff", "--no-textconv", "--no-renames", snapshot.Base, snapshot.Commit, "--"); err != nil {
		return nil, err
	}
	snapshot.Patch = patch.String()
	if err := os.WriteFile(filepath.Join(root, "unified.patch"), patch.Bytes(), 0600); err != nil {
		return nil, err
	}
	metadata, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(root, "snapshot.json"), metadata, 0600); err != nil {
		return nil, err
	}
	j.snapshot = snapshot
	return snapshot, nil
}
