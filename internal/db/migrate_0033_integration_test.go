//go:build db_integration

package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestMigration0033SchedulerIdentityPurgeKind proves migration 0033 widens the
// aura.scheduler_tasks.kind CHECK to admit 'identity_purge' (so the D-27 grace-window
// purge sweep is schedulable) and that its down + re-up is clean:
//
//	(a) after HEAD an INSERT with kind='identity_purge' SUCCEEDS (the widened CHECK admits it);
//	(b) positioned at v32 (before 0033) the same INSERT FAILS with a check_violation (23514);
//	(c) the 0033 down (pre-deletes purge rows, restores the 0010 CHECK) + re-up straddle is clean.
func TestMigration0033SchedulerIdentityPurgeKind(t *testing.T) {
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

	const freshDB = "aura_migrate0033_drill"
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
	head := currentMigrationVersion(t, ctx, freshAdmin)
	if head < 33 {
		t.Fatalf("full Migrate up reached version %d, want at least 33", head)
	}

	app, err := Open(ctx, &Config{URL: dsn("aura_app", freshDB)})
	if err != nil {
		t.Fatalf("open app pool: %v", err)
	}
	defer app.Close()

	// (a) After HEAD: the widened CHECK admits an identity_purge task INSERT.
	if err := insertPurgeTask0033(ctx, app); err != nil {
		t.Fatalf("insert identity_purge task after HEAD: want success, got %v", err)
	}

	// Position DOWN to exactly v32 before the straddle. golang-migrate Steps(n) is
	// RELATIVE to the current head, so a bare -1 reverses whatever is newest; anchoring
	// off `head` isolates 0033's OWN down regardless of how many migrations sit above it.
	// The 0033 down pre-deletes the identity_purge row just inserted, so the re-added
	// narrower (0010) CHECK cannot fail on it — this exercises the down's row-cleanup too.
	// MigrateSteps counts MIGRATIONS, not version numbers. The embedded sequence has
	// gaps since the adaptive plane's twenty-six migrations were removed, so
	// `32 - head` overshot by exactly the size of the gap and golang-migrate
	// refused with an opaque "limit N short". Ask the catalog how far back it is.
	stepsAbove32, err := MigrationStepsAbove(32)
	if err != nil {
		t.Fatalf("MigrationStepsAbove(32): %v", err)
	}
	stepDownToV32 := -stepsAbove32
	if err := MigrateSteps(ctx, migrateURL, stepDownToV32); err != nil {
		t.Fatalf("MigrateSteps(%d) position down to v32: %v", stepDownToV32, err)
	}

	// (b) Before 0033 (at v32): the narrower CHECK rejects identity_purge with 23514.
	err = insertPurgeTask0033(ctx, app)
	if err == nil {
		t.Fatalf("insert identity_purge task at v32: want check_violation, got success")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("insert identity_purge task at v32: want check_violation (23514), got %v", err)
	}

	// (c) Re-up 0033 (v32 -> v33): the widen returns; the INSERT succeeds again.
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("MigrateSteps(+1) re-up 0033: %v", err)
	}
	if err := insertPurgeTask0033(ctx, app); err != nil {
		t.Fatalf("insert identity_purge task after re-up 0033: want success, got %v", err)
	}

	// (c) Straddle down+up at v33: down (pre-deletes purge rows + narrows) then re-up.
	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("MigrateSteps(-1) down 0033: %v", err)
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("MigrateSteps(+1) re-up 0033 (straddle): %v", err)
	}
	if err := insertPurgeTask0033(ctx, app); err != nil {
		t.Fatalf("insert identity_purge task after straddle: want success, got %v", err)
	}

	// Restore to HEAD: re-applying 0034..HEAD (none today) is a clean no-op.
	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("post-round-trip Migrate back to HEAD: %v", err)
	}
	n, err := Migrate(ctx, migrateURL)
	if err != nil {
		t.Fatalf("post-round-trip no-op Migrate: %v", err)
	}
	if n != 0 {
		t.Errorf("post-round-trip no-op Migrate: want 0 pending (0033 reversible), got %d", n)
	}
}

// insertPurgeTask0033 inserts a minimal 'identity_purge' scheduler task on the aura_app
// pool (the runtime role). A fresh random UUID id keeps each call independent, so the
// same helper serves both the success (widened CHECK) and failure (narrow CHECK → 23514)
// assertions without a stale-row collision.
func insertPurgeTask0033(ctx context.Context, pool *pgxpool.Pool) error {
	id := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	_, err := pool.Exec(ctx,
		`INSERT INTO aura.scheduler_tasks (id, kind, schedule_kind, every_minutes) VALUES ($1, 'identity_purge', 'every', 15)`,
		id)
	return err
}
