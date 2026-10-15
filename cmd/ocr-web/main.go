// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/alibaba/open-code-review/internal/platform"
)

func main() {
	if err := run(); err != nil {
		slog.Error("workbench stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := platform.DefaultConfig()
	flag.StringVar(&cfg.Addr, "addr", cfg.Addr, "loopback IP and port")
	flag.StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "directory containing the SQLite database")
	flag.Parse()
	if flag.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()
	lock, err := platform.LockDataDirectory(cfg.DataDir)
	if err != nil {
		return err
	}
	defer lock.Close()
	initCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	store, err := platform.OpenStore(initCtx, cfg.DataDir)
	cancel()
	if err != nil {
		return err
	}
	defer store.Close()
	app, err := platform.NewServer(cfg, store, slog.Default())
	if err != nil {
		return err
	}
	server := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), shutdownSignals()...)
	defer stop()
	syncDone := make(chan struct{})
	go func() { defer close(syncDone); app.RunRepositorySync(ctx) }()
	defer func() { stop(); <-syncDone }()
	finished := make(chan error, 1)
	go func() { finished <- server.Serve(listener) }()
	slog.Info("workbench ready", "url", "http://"+cfg.Addr, "data_dir", cfg.DataDir)
	select {
	case err := <-finished:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return err
		}
		return nil
	}
}
