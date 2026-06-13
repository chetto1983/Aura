//go:build db_integration

// Integration coverage for MigrateSteps — the reversibility seam (n>0 up, n<0
// down). Runs the down+re-up round-trip on a throwaway database so the shared
// `aura` DB (migrated by sibling tests) is untouched, mirroring
// TestMigrate_Phase4_AppliesAndSeeds.
//
// Run via: go test -tags db_integration ./internal/db -run TestMigrateSteps -count=1

package db

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestMigrateSteps_EmptyURLErrors(t *testing.T) {
	if err := MigrateSteps(context.Background(), "", -1); err == nil {
		t.Fatal("MigrateSteps with empty URL: want error, got nil")
	}
}

func TestMigrateSteps_DownUpReversible(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	if err := EnsureRoles(ctx, bootstrapURL(t), pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}

	const freshDB = "aura_migratesteps_drill"
	dsn := func(role, db string) string {
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", role, pwd, host, port, db)
	}

	admin, err := Open(ctx, &Config{URL: dsn("aura", "aura")})
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	defer admin.Close()
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+freshDB+" WITH (FORCE)"); err != nil {
		t.Fatalf("pre-drop fresh db: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+freshDB); err != nil {
		t.Fatalf("create fresh db: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+freshDB+" WITH (FORCE)")
	})
	if _, err := admin.Exec(ctx, "GRANT CREATE ON DATABASE "+freshDB+" TO aura_migrate"); err != nil {
		t.Fatalf("grant create on fresh db: %v", err)
	}
	freshAdmin, err := Open(ctx, &Config{URL: dsn("aura", freshDB)})
	if err != nil {
		t.Fatalf("open fresh-db admin pool: %v", err)
	}
	defer freshAdmin.Close()
	if _, err := freshAdmin.Exec(ctx, "GRANT CREATE ON SCHEMA public TO aura_migrate"); err != nil {
		t.Fatalf("grant create on public schema: %v", err)
	}

	migrateURL := dsn("aura_migrate", freshDB)

	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("full Migrate up on fresh db: %v", err)
	}
	// Roll back the most-recent migration, then re-apply it.
	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("MigrateSteps(-1) down: %v", err)
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("MigrateSteps(+1) re-up: %v", err)
	}
	// After the round-trip the schema must be fully re-materialized: a final
	// Migrate applies zero.
	n, err := Migrate(ctx, migrateURL)
	if err != nil {
		t.Fatalf("post-round-trip Migrate: %v", err)
	}
	if n != 0 {
		t.Errorf("post-round-trip Migrate: want 0 pending (schema reversible), got %d", n)
	}
}
