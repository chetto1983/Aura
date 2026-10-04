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

func TestMigration0048RoundTrip(t *testing.T) {
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
	const name = "aura_migrate0048_drill"
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
	// Migrate lands on the latest version, which may be newer than 0048. Position
	// the database at 48 so the down/re-up below always straddles the migration
	// under test instead of whichever migration happens to be newest.
	for {
		got := currentMigrationVersion(t, ctx, dbAdmin)
		if got == 48 {
			break
		}
		if got < 48 {
			t.Fatalf("version=%d, could not descend to 48", got)
		}
		if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
			t.Fatalf("step down to 48: %v", err)
		}
	}
	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("down: %v", err)
	}
	if got := currentMigrationVersion(t, ctx, dbAdmin); got != 47 {
		t.Fatalf("version after down=%d, want 47", got)
	}
	var phaseExists bool
	if err := dbAdmin.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM information_schema.columns
		WHERE table_schema='aura' AND table_name='conversations' AND column_name='delete_phase'
	)`).Scan(&phaseExists); err != nil {
		t.Fatal(err)
	}
	if phaseExists {
		t.Fatal("delete lifecycle columns survived migration down")
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("re-up: %v", err)
	}
	if got := currentMigrationVersion(t, ctx, dbAdmin); got != 48 {
		t.Fatalf("version after re-up=%d, want 48", got)
	}
}
