package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/envutil"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/ingestsupervisor"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/chetto1983/aura/internal/settings"
)

// routeTimeout bounds one route read's HTTP call -- the sidecar's /v1/models, a hosted
// catalogue -- well inside the supervisor's 15 s tick.
const routeTimeout = 10 * time.Second

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	printEnv := flag.Bool("print-embed-env", false,
		"print the embedding environment a child would receive, credential included, and exit")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, &db.Config{URL: strings.TrimSpace(os.Getenv("AURA_DB_URL"))})
	if err != nil {
		return fmt.Errorf("ingest supervisor database: %w", err)
	}
	defer pool.Close()
	store, err := settings.NewStore(pool, os.Getenv("AURA_AUTHULA_SECRET"))
	if err != nil {
		return fmt.Errorf("ingest supervisor settings: %w", err)
	}
	routes := &ingestsupervisor.RouteResolver{
		Store: store,
		// This process never overlays aura.settings onto its environment, so this is the
		// pre-overlay environment a deleted row must fall back to (spec §6).
		LookupEnv:  os.LookupEnv,
		Dimensions: envutil.IntDefault("AURA_EMBED_DIMENSIONS", config.DefaultEmbedDimensions),
		HTTP:       &http.Client{Timeout: routeTimeout},
	}
	if *printEnv {
		return printEmbedEnv(ctx, routes)
	}
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
		identity.New(pool), resolver, routes, ingestsupervisor.NewExecLauncher(),
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

// printEmbedEnv is how a script that runs `python -m ingest.app` directly hands its child the
// route a supervised child would get: this resolution, never a space the script made up.
func printEmbedEnv(ctx context.Context, routes *ingestsupervisor.RouteResolver) error {
	route, err := routes.Resolve(ctx)
	if err != nil {
		return err
	}
	for _, entry := range route.Environment() {
		fmt.Println(entry)
	}
	return nil
}

func pollInterval(raw string) time.Duration {
	if interval, err := time.ParseDuration(strings.TrimSpace(raw)); err == nil && interval > 0 {
		return interval
	}
	return 0
}
