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
	"text/tabwriter"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
)

func runDB(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: aura db {migrate|ping|status|reset}")
		os.Exit(1)
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config load:", err)
		os.Exit(1)
	}
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
	// Best-effort EnsureRoles bootstrap: only run when POSTGRES_PASSWORD is set
	// (the standard compose-managed case). When operators run against an
	// externally-provisioned cluster where the aura_app/aura_migrate roles
	// already exist with their own credentials, EnsureRoles is skipped.
	if pwd := os.Getenv("POSTGRES_PASSWORD"); pwd != "" {
		bootstrap := os.Getenv("AURA_DB_BOOTSTRAP_URL")
		if bootstrap == "" {
			bootstrap = fmt.Sprintf("postgres://aura:%s@127.0.0.1:5432/aura?sslmode=disable", pwd)
		}
		if err := db.EnsureRoles(ctx, bootstrap, pwd, pwd); err != nil {
			fmt.Fprintln(os.Stderr, "ensure roles:", err)
			os.Exit(1)
		}
	}

	n, err := db.Migrate(ctx, cfg.DB.MigrateURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if n == 0 {
		fmt.Println("ok: no pending migrations")
		return
	}
	fmt.Printf("ok: %d migration(s) applied\n", n)
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
	pool, err := db.Open(ctx, &cfg.DB)
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
	fmt.Fprintln(w, "VERSION\tDIRTY")
	for _, r := range rows {
		fmt.Fprintf(w, "%d\t%v\n", r.Version, r.Dirty)
	}
	_ = w.Flush()
}

func dbReset(ctx context.Context, cfg *config.Config, extra []string) {
	if !hasYesFlag(extra) || os.Getenv("AURA_RESET_YES") != "1" {
		fmt.Fprintln(os.Stderr, "refusing — pass --yes AND set AURA_RESET_YES=1 to confirm destructive reset")
		os.Exit(1)
	}
	if err := db.Reset(ctx, cfg.DB.MigrateURL); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("ok: schema reset")
}

func hasYesFlag(args []string) bool {
	for _, a := range args {
		if a == "--yes" {
			return true
		}
	}
	return false
}
