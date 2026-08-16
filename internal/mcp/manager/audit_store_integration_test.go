//go:build db_integration

// Integration tests for the append-only MCP-audit store (migration 0022 +
// MCPAuditStore). Requires a running Postgres with the migrations applied through
// 0022:
//
//	make db-migrate                         # or the WSL equivalent
//	AURA_DB_URL + AURA_DB_MIGRATE_URL + POSTGRES_PASSWORD set in env
//
// Run via:
//
//	go test -tags db_integration -race ./internal/mcp/manager -count=1
//
// No-skip-as-green: envOrSkip t.Fatals under $CI when the DSN is unset, so a
// skipped tier can never pass as green in the pipeline.
package manager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/dbtest"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// envOrSkip mirrors internal/skills + internal/identity: skip locally, fail-loud
// under CI so a missing DSN never reports a falsely-green integration job.
func envOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("integration test requires %s, but it is unset under CI — "+
				"a skipped integration test must not pass as green; wire it in ci.yml", key)
		}
		t.Skipf("integration test requires %s; set it and re-run (e.g. via .env + make db-up)", key)
	}
	return v
}

// bootstrapURL composes the superuser DSN EnsureRoles needs from the password.
func bootstrapURL(t *testing.T, pwd string) string {
	t.Helper()
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

// migratedPool ensures roles + migrations (to head) are applied, then returns an
// aura_app pool ready for MCPAuditStore use. Closed via t.Cleanup.
func migratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := dbtest.MigrateURL(t, envOrSkip(t, "AURA_DB_MIGRATE_URL"))
	appURL := envOrSkip(t, "AURA_DB_URL")

	if err := db.EnsureRoles(ctx, bootstrapURL(t, pwd), pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	if _, err := db.Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := db.Open(ctx, &db.Config{URL: appURL})
	if err != nil {
		t.Fatalf("Open (aura_app): %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TestMCPAuditAppendOnly proves the append-only guarantee (T-29-01-01): a coherent
// INSERT round-trips (including a NULL reason), and a subsequent UPDATE/DELETE/
// TRUNCATE as aura_app is denied (the triggers raise 42501 AND aura_app lacks the
// grant). The row survives every attempt.
func TestMCPAuditAppendOnly(t *testing.T) {
	pool := migratedPool(t)
	store := NewMCPAuditStore(pool)
	ctx := context.Background()

	server := "mcp-rt-" + uuid.Must(uuid.NewV7()).String()
	got, err := store.InsertMCPAudit(ctx, MCPAuditInsert{
		ActorIdentityID: "local",
		Action:          "install",
		ServerName:      server,
		// reason intentionally empty -> NULL column (NULL except on `trust`).
	})
	if err != nil {
		t.Fatalf("InsertMCPAudit: %v", err)
	}
	if got.ID == "" || got.CreatedAt.IsZero() {
		t.Fatalf("InsertMCPAudit: want id+created_at populated, got %+v", got)
	}
	if got.ServerName != server || got.Action != "install" {
		t.Errorf("round-trip: want server=%s action=install, got server=%s action=%s", server, got.ServerName, got.Action)
	}
	if got.Reason != "" {
		t.Errorf("empty reason must project to \"\" (NULL column), got %q", got.Reason)
	}

	// A `trust` row carries a non-NULL reason that round-trips.
	trust, err := store.InsertMCPAudit(ctx, MCPAuditInsert{
		ActorIdentityID: "local",
		Action:          "trust",
		ServerName:      server,
		Reason:          "operator vetted the publisher",
	})
	if err != nil {
		t.Fatalf("InsertMCPAudit (trust): %v", err)
	}
	if trust.Reason != "operator vetted the publisher" {
		t.Errorf("reason round-trip: want vetted note, got %q", trust.Reason)
	}

	// aura_app must NOT be able to mutate history: UPDATE / DELETE / TRUNCATE all denied.
	if _, err := pool.Exec(ctx, "UPDATE aura.mcp_audit SET action = 'tampered' WHERE id = $1", got.ID); err == nil {
		t.Error("UPDATE as aura_app: want denied (trigger or role), got nil error")
	} else if code := sqlState(err); code != "42501" {
		t.Errorf("UPDATE SQLSTATE = %q, want 42501 insufficient_privilege", code)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM aura.mcp_audit WHERE id = $1", got.ID); err == nil {
		t.Error("DELETE as aura_app: want denied (trigger or role), got nil error")
	} else if code := sqlState(err); code != "42501" {
		t.Errorf("DELETE SQLSTATE = %q, want 42501 insufficient_privilege", code)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE aura.mcp_audit"); err == nil {
		t.Error("TRUNCATE as aura_app: want denied (statement trigger or role), got nil error")
	} else if code := sqlState(err); code != "42501" {
		t.Errorf("TRUNCATE SQLSTATE = %q, want 42501 insufficient_privilege", code)
	}

	// Both rows survived every attempt.
	rows, err := store.List(ctx, MCPAuditFilter{ServerName: server})
	if err != nil {
		t.Fatalf("List after mutation attempts: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("audit rows tampered or removed: want 2 rows for %s, got %d", server, len(rows))
	}
	// Newest-first: the `trust` row (inserted second) leads.
	if rows[0].ID != trust.ID || rows[1].ID != got.ID {
		t.Errorf("List order: want newest-first [trust,install], got [%s,%s]", rows[0].Action, rows[1].Action)
	}
}

// TestMigration0022_SchemaRoundTrip migrates back to the 0021 floor, then up to head
// again and asserts the table is gone after down and present after re-up, proving the
// migration is reversible+idempotent even as later migrations are added above 0022.
func TestMigration0022_SchemaRoundTrip(t *testing.T) {
	pool := migratedPool(t) // ensures roles + migrations to head first
	ctx := context.Background()

	migrateURL := dbtest.MigrateURL(t, envOrSkip(t, "AURA_DB_MIGRATE_URL"))
	if err := migrateToVersion(t, ctx, migrateURL, 21); err != nil {
		t.Fatalf("migrate down to 0021: %v", err)
	}
	if tableExists(t, pool, "mcp_audit") {
		t.Error("after down to 0021: aura.mcp_audit still exists")
	}

	// Up to head: the table returns.
	if _, err := db.Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	if !tableExists(t, pool, "mcp_audit") {
		t.Error("after re-up: aura.mcp_audit missing")
	}
}

// sqlState extracts the SQLSTATE from a pgconn.PgError, "" if not a pg error.
func sqlState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// migrateToVersion steps the DB to the target migration version, computing the step
// count from the current head so this test does not rot when 0023+ are added.
func migrateToVersion(t *testing.T, ctx context.Context, migrateURL string, target int64) error {
	t.Helper()
	migratePool, err := db.Open(ctx, &db.Config{URL: migrateURL})
	if err != nil {
		return fmt.Errorf("open migrate pool: %w", err)
	}
	defer migratePool.Close()

	rows, err := db.Status(ctx, migratePool)
	if err != nil {
		return fmt.Errorf("migration status: %w", err)
	}
	if len(rows) == 0 {
		return fmt.Errorf("migration status: no applied migrations")
	}
	current := rows[len(rows)-1].Version
	// `target - current` counts VERSIONS; MigrateSteps counts MIGRATIONS, and the
	// embedded sequence has gaps since the adaptive plane's twenty-six migrations were
	// removed. The difference showed up as golang-migrate's opaque "limit N short".
	steps, err := db.MigrationStepDelta(current, target)
	if err != nil {
		return fmt.Errorf("migration step delta %d -> %d: %w", current, target, err)
	}
	if steps == 0 {
		return nil
	}
	return db.MigrateSteps(ctx, migrateURL, steps)
}

// tableExists reports whether aura.<name> exists.
func tableExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(context.Background(),
		"SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='aura' AND table_name=$1)",
		name,
	).Scan(&exists); err != nil {
		t.Fatalf("tableExists %q: %v", name, err)
	}
	return exists
}
