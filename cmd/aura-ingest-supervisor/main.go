package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/ingestsupervisor"
	"github.com/chetto1983/aura/internal/objectstore"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, &db.Config{URL: strings.TrimSpace(os.Getenv("AURA_DB_URL"))})
	if err != nil {
		return fmt.Errorf("ingest supervisor database: %w", err)
	}
	defer pool.Close()
	resolver, err := objectstore.NewIdentityStore(
		pool,
		os.Getenv("AURA_AUTHULA_SECRET"),
		objectstore.Credentials{
			Bucket:    os.Getenv("AURA_OBJECTSTORE_BUCKET"),
			AccessKey: os.Getenv("AURA_OBJECTSTORE_ACCESS_KEY"),
			SecretKey: os.Getenv("AURA_OBJECTSTORE_SECRET_KEY"),
		},
		identityctx.LocalOperatorIdentity,
	)
	if err != nil {
		return fmt.Errorf("ingest supervisor object store: %w", err)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	supervisor := ingestsupervisor.New(
		identity.New(pool), resolver, ingestsupervisor.NewExecLauncher(),
		ingestsupervisor.Options{
			PollInterval: pollInterval(os.Getenv("AURA_INGEST_SUPERVISOR_INTERVAL")),
			StateRoot:    os.Getenv("AURA_INGEST_STATE_ROOT"),
			S3Endpoint:   os.Getenv("AURA_OBJECTSTORE_ENDPOINT"),
			S3Region:     os.Getenv("AURA_OBJECTSTORE_REGION"),
			Logger:       logger,
		},
	)
	if err := supervisor.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func pollInterval(raw string) time.Duration {
	if interval, err := time.ParseDuration(strings.TrimSpace(raw)); err == nil && interval > 0 {
		return interval
	}
	return 0
}
