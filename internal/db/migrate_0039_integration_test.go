//go:build db_integration

package db

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestMigration0039RoundTrip(t *testing.T) {
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
	const name = "aura_migrate0039_drill"
	dsn := func(role, database string) string {
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", role, pwd, host, port, database)
	}
	admin, err := Open(ctx, &Config{URL: dsn("aura", "aura")})
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)") })
	if _, err := admin.Exec(ctx, "GRANT CREATE ON DATABASE "+name+" TO aura_migrate"); err != nil {
		t.Fatal(err)
	}
	dbAdmin, err := Open(ctx, &Config{URL: dsn("aura", name)})
	if err != nil {
		t.Fatal(err)
	}
	defer dbAdmin.Close()
	if _, err := dbAdmin.Exec(ctx, "GRANT CREATE ON SCHEMA public TO aura_migrate"); err != nil {
		t.Fatal(err)
	}
	migrateURL := dsn("aura_migrate", name)
	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("up: %v", err)
	}
	if got := currentMigrationVersion(t, ctx, dbAdmin); got != 39 {
		t.Fatalf("version=%d, want 39", got)
	}
	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("down: %v", err)
	}
	if got := currentMigrationVersion(t, ctx, dbAdmin); got != 38 {
		t.Fatalf("version after down=%d, want 38", got)
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("re-up: %v", err)
	}
	if got := currentMigrationVersion(t, ctx, dbAdmin); got != 39 {
		t.Fatalf("version after re-up=%d, want 39", got)
	}
}
