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
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
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
	if err := EnsureRoles(ctx, bootstrapURL(t), os.Getenv("POSTGRES_PASSWORD")); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	migrateURL := envOrSkip(t, "AURA_DB_MIGRATE_URL")

	// Roles must exist before m.Up runs.
	if err := EnsureRoles(ctx, bootstrapURL(t), os.Getenv("POSTGRES_PASSWORD")); err != nil {
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
	if err := EnsureRoles(ctx, bootstrapURL(t), os.Getenv("POSTGRES_PASSWORD")); err != nil {
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
	// The security property is "aura_app is denied DDL", not a specific message.
	// Postgres reports privilege failures with SQLSTATE 42501 (insufficient_privilege)
	// regardless of whether the wording is "permission denied for ..." (TRUNCATE /
	// CREATE) or "must be owner of table ..." (DROP). Asserting the SQLSTATE is both
	// the actual property under test and stable across operations and PG versions.
	const insufficientPrivilege = "42501"
	for _, stmt := range denied {
		_, err := pool.Exec(ctx, stmt)
		if err == nil {
			t.Errorf("aura_app: %q unexpectedly succeeded — role separation broken", stmt)
			continue
		}
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != insufficientPrivilege {
			t.Errorf("aura_app: %q error = %q, want SQLSTATE %s (insufficient_privilege)",
				stmt, err.Error(), insufficientPrivilege)
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
	if err := EnsureRoles(ctx, bootstrapURL(t), os.Getenv("POSTGRES_PASSWORD")); err != nil {
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

func TestStatus_ReturnsAppliedMigrations(t *testing.T) {
	// Status reads golang-migrate's public.schema_migrations tracker via the
	// migrate role (per db.go comment). After a clean Migrate the tracker must
	// list the applied versions in ascending order with no dirty marker.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	migrateURL := envOrSkip(t, "AURA_DB_MIGRATE_URL")
	if err := EnsureRoles(ctx, bootstrapURL(t), os.Getenv("POSTGRES_PASSWORD")); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	pool, err := Open(ctx, &Config{URL: migrateURL})
	if err != nil {
		t.Fatalf("Open (migrate role): %v", err)
	}
	defer pool.Close()

	rows, err := Status(ctx, pool)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("Status: want >= 1 applied migration row, got 0")
	}
	for i := 1; i < len(rows); i++ {
		if rows[i].Version < rows[i-1].Version {
			t.Errorf("Status rows not ascending: version %d follows %d", rows[i].Version, rows[i-1].Version)
		}
	}
	if last := rows[len(rows)-1]; last.Dirty {
		t.Errorf("Status: latest migration (version %d) marked dirty after a clean Migrate", last.Version)
	}
}

func TestPing_QueryErrorOnCanceledContext(t *testing.T) {
	// Open a live pool, then cancel the context so the SELECT 1 fails — exercises
	// Ping's query-error branch against a real (non-nil) pool.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := Open(ctx, &Config{URL: envOrSkip(t, "AURA_DB_URL")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer pool.Close()

	canceled, cancelNow := context.WithCancel(ctx)
	cancelNow()
	if _, err := Ping(canceled, pool); err == nil {
		t.Error("Ping with canceled context: want error, got nil")
	}
}

func TestStatus_SurfacesInaccessibleTrackerError(t *testing.T) {
	// aura_app has no SELECT grant on public.schema_migrations (role separation).
	// pgx executes pool.Query lazily, so the permission error surfaces during
	// iteration (rows.Err), not at Query time — Status must propagate it as a
	// wrapped "status rows" error rather than silently masking a real failure.
	//
	// NOTE: Status's lines 32-36 ("immediate Query error => empty slice") are
	// effectively unreachable with pgx pooled queries for the same lazy-exec
	// reason; the documented "missing table => empty" contract is not honored.
	// Tracked as a separate latent bug, out of scope for this coverage pass.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := Open(ctx, &Config{URL: envOrSkip(t, "AURA_DB_URL")}) // aura_app
	if err != nil {
		t.Fatalf("Open as aura_app: %v", err)
	}
	defer pool.Close()

	_, err = Status(ctx, pool)
	if err == nil {
		t.Fatal("Status (inaccessible tracker): want a surfaced error, got nil")
	}
	if !strings.Contains(err.Error(), "status") {
		t.Errorf("Status error: want 'status' context wrap, got %q", err.Error())
	}
}

func TestEnsureRoles_NonPrivilegedBootstrapDenied(t *testing.T) {
	// Driving EnsureRoles with the aura_app DSN (no CREATEROLE) must surface a
	// privilege error from the ALTER/CREATE ROLE Exec, with the password redacted.
	// Passing the shared password keeps aura_app's own credential unchanged.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	appURL := envOrSkip(t, "AURA_DB_URL")

	err := EnsureRoles(ctx, appURL, pwd)
	if err == nil {
		t.Fatal("EnsureRoles via aura_app: want privilege error, got nil")
	}
	if strings.Contains(err.Error(), pwd) {
		t.Errorf("EnsureRoles error leaked password: %q", err.Error())
	}
}

func TestReset_DownUpRoundTrip(t *testing.T) {
	// Reset rolls every migration Down then back Up. A follow-up Migrate must be
	// a no-op (0 newly-applied), proving the schema was fully re-materialized.
	// Relies on 0001_init.down.sql NOT dropping roles (role lifecycle is owned by
	// EnsureRoles) so the Up's GRANTs land on the still-present roles.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	migrateURL := envOrSkip(t, "AURA_DB_MIGRATE_URL")
	if err := EnsureRoles(ctx, bootstrapURL(t), os.Getenv("POSTGRES_PASSWORD")); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("pre-Reset Migrate: %v", err)
	}

	if err := Reset(ctx, migrateURL); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	n, err := Migrate(ctx, migrateURL)
	if err != nil {
		t.Fatalf("post-Reset Migrate: %v", err)
	}
	if n != 0 {
		t.Errorf("post-Reset Migrate: want 0 pending (schema fully re-applied), got %d", n)
	}
}
