// db subcommand dispatcher for `aura db {migrate|ping|status|reset}`. Lives
// in package main alongside cmd/aura/main.go's switch case "db". Each branch
// loads config, opens what it needs (pool for ping/status; migrate URL for
// migrate/reset), prints a human-readable status line, exits non-zero on
// error. Per D-07: migrate/reset require AURA_DB_MIGRATE_URL.
package main

import (
	"context"
	"fmt"
	"os"
	"slices"
	"text/tabwriter"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
)

func runDB(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: aura db {migrate|ping|status|reset}")
		os.Exit(1)
	}
	// DB-admin commands are pure DB operations — use the LLM-free config load so
	// `aura db migrate` does not require OPENROUTER_API_KEY (e.g. CI's migrate step).
	cfg := config.LoadDB()
	ctx := context.Background()

	switch args[0] {
	case "migrate":
		dbMigrate(ctx, cfg)
	case "ping":
		dbPing(ctx, cfg)
	case "status":
		dbStatus(ctx, cfg)
	case "reset":
		dbReset(ctx, cfg, args[1:])
	default:
		fmt.Fprintln(os.Stderr, "usage: aura db {migrate|ping|status|reset}")
		os.Exit(1)
	}
}

func dbMigrate(ctx context.Context, cfg *config.Config) {
	lockURL := cfg.DB.MigrateURL
	if cfg.DB.BootstrapURL != "" && cfg.DB.Password != "" {
		lockURL = cfg.DB.BootstrapURL
	}
	var applied int
	err := db.WithMigrationLock(ctx, lockURL, func(ctx context.Context) error {
		// The bootstrap role owns the lock before EnsureRoles so two fresh-install
		// commands cannot race role creation. Managed clusters without bootstrap
		// credentials acquire the same lock through the pre-provisioned migrate role.
		if cfg.DB.BootstrapURL != "" && cfg.DB.Password != "" {
			if err := db.EnsureRoles(ctx, cfg.DB.BootstrapURL, cfg.DB.Password); err != nil {
				return fmt.Errorf("ensure roles: %w", err)
			}
		}
		var err error
		applied, err = db.Migrate(ctx, cfg.DB.MigrateURL)
		if err != nil {
			return err
		}
		return db.CheckMigrationHead(ctx, cfg.DB.MigrateURL)
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if applied == 0 {
		fmt.Println("ok: no pending migrations")
		return
	}
	fmt.Printf("ok: %d migration(s) applied\n", applied)
}

func dbPing(ctx context.Context, cfg *config.Config) {
	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer pool.Close()
	latency, err := db.Ping(ctx, pool)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("ok: ping in %s\n", latency)
}

func dbStatus(ctx context.Context, cfg *config.Config) {
	// Status reads golang-migrate's `schema_migrations` tracker in `public`,
	// which is owned by aura_migrate and not readable by aura_app under our
	// role-separation policy. Open with the migrate DSN instead — observing
	// migration state is a maintenance operation, not a runtime path.
	statusCfg := cfg.DB
	if cfg.DB.MigrateURL != "" {
		statusCfg.URL = cfg.DB.MigrateURL
	}
	pool, err := db.Open(ctx, &statusCfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer pool.Close()
	rows, err := db.Status(ctx, pool)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(rows) == 0 {
		fmt.Println("ok: no migrations applied yet (schema_migrations is empty or missing)")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "VERSION\tDIRTY")
	for _, r := range rows {
		_, _ = fmt.Fprintf(w, "%d\t%v\n", r.Version, r.Dirty)
	}
	_ = w.Flush()
}

func dbReset(ctx context.Context, cfg *config.Config, extra []string) {
	if !slices.Contains(extra, "--yes") || os.Getenv("AURA_RESET_YES") != "1" {
		fmt.Fprintln(os.Stderr, "refusing — pass --yes AND set AURA_RESET_YES=1 to confirm destructive reset")
		os.Exit(1)
	}
	if err := db.Reset(ctx, cfg.DB.MigrateURL); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("ok: schema reset")
}
