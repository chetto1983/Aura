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

// TestMigration0034SchedulerSandboxReapKind proves migration 0034 widens the
// aura.scheduler_tasks.kind CHECK to admit 'sandbox_reap' (so the D-08 idle-suspend reap
// sweep is schedulable) and that its down + re-up is clean:
//
//	(a) after HEAD an INSERT with kind='sandbox_reap' SUCCEEDS (the widened CHECK admits it);
//	(b) positioned at v33 (before 0034) the same INSERT FAILS with a check_violation (23514);
//	(c) the 0034 down (pre-deletes reap rows, restores the 0033 CHECK) + re-up straddle is clean.
func TestMigration0034SchedulerSandboxReapKind(t *testing.T) {
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

	const freshDB = "aura_migrate0034_drill"
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
	if head < 34 {
		t.Fatalf("full Migrate up reached version %d, want at least 34", head)
	}

	app, err := Open(ctx, &Config{URL: dsn("aura_app", freshDB)})
	if err != nil {
		t.Fatalf("open app pool: %v", err)
	}
	defer app.Close()

	// (a) After HEAD: the widened CHECK admits a sandbox_reap task INSERT.
	if err := insertReapTask0034(ctx, app); err != nil {
		t.Fatalf("insert sandbox_reap task after HEAD: want success, got %v", err)
	}

	// Position DOWN to exactly v33 before the straddle. golang-migrate Steps(n) is RELATIVE
	// to the current head, so a bare -1 reverses whatever is newest; anchoring off `head`
	// isolates 0034's OWN down regardless of how many migrations sit above it. The 0034 down
	// pre-deletes the sandbox_reap row just inserted, so the re-added narrower (0033) CHECK
	// cannot fail on it — this exercises the down's row-cleanup too.
	// MigrateSteps counts MIGRATIONS, not version numbers. The embedded sequence has
	// gaps since the adaptive plane's twenty-six migrations were removed, so
	// `33 - head` overshot by exactly the size of the gap and golang-migrate
	// refused with an opaque "limit N short". Ask the catalog how far back it is.
	stepsAbove33, err := MigrationStepsAbove(33)
	if err != nil {
		t.Fatalf("MigrationStepsAbove(33): %v", err)
	}
	stepDownToV33 := -stepsAbove33
	if err := MigrateSteps(ctx, migrateURL, stepDownToV33); err != nil {
		t.Fatalf("MigrateSteps(%d) position down to v33: %v", stepDownToV33, err)
	}

	// (b) Before 0034 (at v33): the narrower CHECK rejects sandbox_reap with 23514.
	err = insertReapTask0034(ctx, app)
	if err == nil {
		t.Fatalf("insert sandbox_reap task at v33: want check_violation, got success")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("insert sandbox_reap task at v33: want check_violation (23514), got %v", err)
	}

	// (c) Re-up 0034 (v33 -> v34): the widen returns; the INSERT succeeds again.
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("MigrateSteps(+1) re-up 0034: %v", err)
	}
	if err := insertReapTask0034(ctx, app); err != nil {
		t.Fatalf("insert sandbox_reap task after re-up 0034: want success, got %v", err)
	}

	// (c) Straddle down+up at v34: down (pre-deletes reap rows + narrows) then re-up.
	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("MigrateSteps(-1) down 0034: %v", err)
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("MigrateSteps(+1) re-up 0034 (straddle): %v", err)
	}
	if err := insertReapTask0034(ctx, app); err != nil {
		t.Fatalf("insert sandbox_reap task after straddle: want success, got %v", err)
	}

	// Restore to HEAD: re-applying 0035..HEAD (none today) is a clean no-op.
	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("post-round-trip Migrate back to HEAD: %v", err)
	}
	n, err := Migrate(ctx, migrateURL)
	if err != nil {
		t.Fatalf("post-round-trip no-op Migrate: %v", err)
	}
	if n != 0 {
		t.Errorf("post-round-trip no-op Migrate: want 0 pending (0034 reversible), got %d", n)
	}
}

// insertReapTask0034 inserts a minimal 'sandbox_reap' scheduler task on the aura_app pool (the
// runtime role). A fresh random UUID id keeps each call independent, so the same helper serves
// both the success (widened CHECK) and failure (narrow CHECK → 23514) assertions without a
// stale-row collision.
func insertReapTask0034(ctx context.Context, pool *pgxpool.Pool) error {
	id := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	_, err := pool.Exec(ctx,
		`INSERT INTO aura.scheduler_tasks (id, kind, schedule_kind, every_minutes, notify_route) VALUES ($1, 'sandbox_reap', 'every', 15, 'none')`,
		id)
	return err
}
