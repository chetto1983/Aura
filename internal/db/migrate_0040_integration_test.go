//go:build db_integration

package db

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/dbtest"
)

func TestMigration0040RoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	host, port := os.Getenv("PGHOST"), os.Getenv("PGPORT")
	if host == "" {
		host = "127.0.0.1"
	}
	if port == "" {
		port = "5432"
	}
	if err := EnsureRoles(ctx, bootstrapURL(t), pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	const name = "aura_migrate0040_drill"
	dsn := func(role, database string) string {
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", role, pwd, host, port, database)
	}
	dbtest.DrillDatabase(t, dsn("aura", "aura"), name)
	dbAdmin, err := Open(ctx, &Config{URL: dsn("aura", name)})
	if err != nil {
		t.Fatal(err)
	}
	defer dbAdmin.Close()
	migrateURL := dsn("aura_migrate", name)
	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("up: %v", err)
	}
	// Migrate lands on the latest version (>=41 once 0041 shared_links RLS shipped). Step down
	// to 40 so this 0040 down/re-up roundtrip exercises the migration under test regardless
	// of how many migrations sit above it — the test is version-anchored, not latest-anchored.
	for {
		got := currentMigrationVersion(t, ctx, dbAdmin)
		if got == 40 {
			break
		}
		if got < 40 {
			t.Fatalf("version=%d, could not descend to 40", got)
		}
		if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
			t.Fatalf("step down to 40: %v", err)
		}
	}
	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("down: %v", err)
	}
	if got := currentMigrationVersion(t, ctx, dbAdmin); got != 39 {
		t.Fatalf("version after down=%d, want 39", got)
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("re-up: %v", err)
	}
	if got := currentMigrationVersion(t, ctx, dbAdmin); got != 40 {
		t.Fatalf("version after re-up=%d, want 40", got)
	}
}
