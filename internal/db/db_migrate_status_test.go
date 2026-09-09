//go:build db_integration

// Migration-tracker reads for internal/db: what Status reports once migrations have been
// applied, and how it behaves when the tracker table is missing or unreadable. Split out of
// db_test.go on touch (CLAUDE.md: no file over 600 LOC) -- these four exercise the reporting
// surface, not the applying of migrations, which stays in db_test.go.
package db

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/dbtest"
)

func TestStatus_ReturnsAppliedMigrations(t *testing.T) {
	// Status reads golang-migrate's public.schema_migrations tracker via the
	// migrate role (per db.go comment). After a clean Migrate the tracker must
	// list the applied versions in ascending order with no dirty marker.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	migrateURL := dbtest.MigrateURL(t, envOrSkip(t, "AURA_DB_MIGRATE_URL"))
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

func TestCheckMigrationHeadAcceptsCleanEmbeddedHead(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	migrateURL := dbtest.MigrateURL(t, envOrSkip(t, "AURA_DB_MIGRATE_URL"))
	if err := EnsureRoles(ctx, bootstrapURL(t), os.Getenv("POSTGRES_PASSWORD")); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := CheckMigrationHead(ctx, migrateURL); err != nil {
		t.Fatalf("CheckMigrationHead: %v", err)
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
	// This is distinct from the missing-table case (SQLSTATE 42P01), which Status
	// deliberately maps to an empty slice (see TestStatus_MissingTableReturnsEmpty).
	// A privilege error (42501) is a real failure and must surface.
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

func TestStatus_MissingTableReturnsEmpty(t *testing.T) {
	// On a database where no migration has ever run, public.schema_migrations does
	// not exist (SQLSTATE 42P01). Status must honor its first-boot contract — empty
	// slice, nil error — not surface a "relation does not exist". Uses a throwaway
	// database so the shared `aura` DB (migrated by sibling tests) is untouched.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
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
	const freshDB = "aura_status_empty_drill"
	dsn := func(db string) string {
		return fmt.Sprintf("postgres://aura:%s@%s:%s/%s?sslmode=disable", pwd, host, port, db)
	}

	admin, err := Open(ctx, &Config{URL: dsn("aura")})
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

	fresh, err := Open(ctx, &Config{URL: dsn(freshDB)})
	if err != nil {
		t.Fatalf("open fresh pool: %v", err)
	}
	defer fresh.Close()

	rows, err := Status(ctx, fresh)
	if err != nil {
		t.Fatalf("Status on fresh db: want nil error (42P01 => empty contract), got %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("Status on fresh db: want empty slice, got %d rows", len(rows))
	}
}
