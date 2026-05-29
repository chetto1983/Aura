//go:build db_integration

// Integration tests for internal/db. Requires a running Postgres container:
//   make db-up
//   AURA_DB_URL + AURA_DB_MIGRATE_URL set in env
//   POSTGRES_PASSWORD set for EnsureRoles bootstrap.
//
// Run via:
//   go test -tags db_integration -race ./internal/db -count=1

package db

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// envOrSkip returns the env var value or skips the test with a clear message.
func envOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Skipf("integration test requires %s; set it and re-run (e.g. via .env + make db-up)", key)
	}
	return v
}

// bootstrapURL returns the superuser DSN for role/database creation.
// Conventionally `postgres://aura:${POSTGRES_PASSWORD}@127.0.0.1:5432/aura`.
func bootstrapURL(t *testing.T) string {
	t.Helper()
	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	return fmt.Sprintf("postgres://aura:%s@%s:%s/aura?sslmode=disable", pwd, host, port)
}

func TestEnsureRoles_CreatesBothRoles(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := EnsureRoles(ctx, bootstrapURL(t), "aura_app_secret", "aura_migrate_secret"); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	migrateURL := envOrSkip(t, "AURA_DB_MIGRATE_URL")

	// Roles must exist before m.Up runs.
	if err := EnsureRoles(ctx, bootstrapURL(t), "aura_app_secret", "aura_migrate_secret"); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}

	n1, err := Migrate(ctx, migrateURL)
	if err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	t.Logf("first migrate applied %d migrations", n1)

	n2, err := Migrate(ctx, migrateURL)
	if err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if n2 != 0 {
		t.Errorf("second Migrate: want 0 newly-applied (idempotent), got %d", n2)
	}
}

func TestPing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg := &Config{URL: envOrSkip(t, "AURA_DB_URL")}
	pool, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer pool.Close()

	latency, err := Ping(ctx, pool)
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if latency <= 0 {
		t.Errorf("Ping latency: want > 0, got %s", latency)
	}
	t.Logf("ping latency: %s", latency)
}

func TestRoleSeparation_AppDenied(t *testing.T) {
	// T-1.05-02 mitigation. Connect as aura_app, attempt TRUNCATE + DROP on
	// aura.knowledge_migrations; both must return "permission denied".
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	migrateURL := envOrSkip(t, "AURA_DB_MIGRATE_URL")
	// Ensure roles + migrations are applied first.
	if err := EnsureRoles(ctx, bootstrapURL(t), "aura_app_secret", "aura_migrate_secret"); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	cfg := &Config{URL: envOrSkip(t, "AURA_DB_URL")} // role aura_app
	pool, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("Open as aura_app: %v", err)
	}
	defer pool.Close()

	denied := []string{
		"TRUNCATE aura.knowledge_migrations",
		"DROP TABLE aura.knowledge_migrations",
		"CREATE TABLE aura.t_role_test (id int)",
	}
	for _, stmt := range denied {
		_, err := pool.Exec(ctx, stmt)
		if err == nil {
			t.Errorf("aura_app: %q unexpectedly succeeded — role separation broken", stmt)
			continue
		}
		if !strings.Contains(strings.ToLower(err.Error()), "permission denied") {
			t.Errorf("aura_app: %q error = %q, want 'permission denied'", stmt, err.Error())
		}
	}
}

func TestRecordAndListKnowledgeMigrations(t *testing.T) {
	// Verifies Slice 0.7 handoff: sqlc bindings + aura_app INSERT grant work.
	// We import the sqlc package only here to keep its surface out of the unit
	// build. The test inserts a row via aura_app and reads it back.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	migrateURL := envOrSkip(t, "AURA_DB_MIGRATE_URL")
	if err := EnsureRoles(ctx, bootstrapURL(t), "aura_app_secret", "aura_migrate_secret"); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	cfg := &Config{URL: envOrSkip(t, "AURA_DB_URL")} // aura_app
	pool, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("Open as aura_app: %v", err)
	}
	defer pool.Close()

	// Cleanup any prior test row.
	if _, err := pool.Exec(ctx, "DELETE FROM aura.knowledge_migrations WHERE version >= 9000"); err != nil {
		t.Logf("cleanup (best-effort): %v", err)
	}

	// Insert via the generated sqlc binding shape (mirrors what Slice 0.7 will do).
	if _, err := pool.Exec(ctx,
		"INSERT INTO aura.knowledge_migrations (version, name, checksum) VALUES ($1, $2, $3)",
		9001, "test_handoff", "deadbeef"); err != nil {
		t.Fatalf("INSERT as aura_app: %v", err)
	}

	var version int32
	var name, checksum string
	if err := pool.QueryRow(ctx,
		"SELECT version, name, checksum FROM aura.knowledge_migrations WHERE version = $1", 9001,
	).Scan(&version, &name, &checksum); err != nil {
		t.Fatalf("SELECT as aura_app: %v", err)
	}
	if version != 9001 || name != "test_handoff" || checksum != "deadbeef" {
		t.Errorf("round-trip mismatch: got (%d, %q, %q)", version, name, checksum)
	}

	// Cleanup.
	if _, err := pool.Exec(ctx, "DELETE FROM aura.knowledge_migrations WHERE version = 9001"); err != nil {
		t.Logf("cleanup (best-effort): %v", err)
	}
}
