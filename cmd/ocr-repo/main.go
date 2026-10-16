// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"

	"github.com/alibaba/open-code-review/internal/platform"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	dataDir := flag.String("data-dir", ".ocr-data", "existing workbench data directory")
	action := flag.String("action", "list", "list, fetch, or branches")
	selector := flag.String("repository", "", "repository ID, exact name, or owner/name")
	localSource := flag.String("local-source", "", "local bare repository for fetch, without HTTP or credentials")
	flag.Parse()
	if flag.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if *action != "list" && *action != "fetch" && *action != "branches" {
		return errors.New("action must be list, fetch, or branches")
	}
	if *localSource != "" && *action != "fetch" {
		return errors.New("local-source is only supported for fetch")
	}
	dir, err := filepath.Abs(*dataDir)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dir, "workbench.db")); err != nil {
		return errors.New("existing workbench.db not found; check -data-dir")
	}
	if *action != "list" {
		lock, err := platform.LockDataDirectory(dir)
		if err != nil {
			return fmt.Errorf("stop the workbench before direct cache maintenance (or use scripts/repositories/manage.py --pause-workbench): %w", err)
		}
		defer lock.Close()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	store, err := platform.OpenStore(ctx, dir)
	if err != nil {
		return err
	}
	defer store.Close()
	values, err := store.ManagedRepositories(ctx, dir)
	if err != nil {
		return err
	}
	if *action == "list" {
		return json.NewEncoder(os.Stdout).Encode(values)
	}
	var matches []platform.ManagedRepository
	for _, v := range values {
		if *selector == strconv.FormatInt(v.ID, 10) || *selector == v.Name || *selector == v.FullName {
			matches = append(matches, v)
		}
	}
	if len(matches) != 1 {
		return errors.New("specify one unambiguous repository ID, exact name, or owner/name; use list first")
	}
	v := matches[0]
	fmt.Fprintf(os.Stderr, "%s: %s\nCache: %s\n", *action, v.FullName, v.CachePath)
	if err := store.MaintainRepository(ctx, dir, v.ID, *action, *localSource, os.Stdout); err != nil {
		return err
	}
	if *action == "fetch" {
		fmt.Fprintln(os.Stdout, "Fetch completed. Workbench sync status updated.")
	}
	return nil
}
