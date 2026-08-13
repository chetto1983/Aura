//go:build db_integration

package db

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// TestEnsureRolesMakesAuthulaReadableByMigrate covers the backup outage measured on the
// live deployment on 2026-08-13: five nightly pg_dump runs, five failures, zero dumps.
//
// Authula creates its own tables at runtime as aura_app, so aura_app owns them. 0019
// grants aura_app CREATE+USAGE on the schema and stops there, reasoning that the owner
// already has DML — which forgot that pg_dump runs as aura_migrate. Owning the SCHEMA
// conveys nothing over another role's tables inside it, so the dump could not even LOCK
// them and aborted on the first one it reached.
//
// Both halves are asserted, because neither covers the other:
//   - a table that already exists when EnsureRoles runs (the GRANT ... ON ALL TABLES half)
//   - a table created AFTER it (the ALTER DEFAULT PRIVILEGES half). Without this, the next
//     Authula upgrade that adds a plugin table silently re-breaks the backup.
func TestEnsureRolesMakesAuthulaReadableByMigrate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, _, appURL := fresh0093Database(t, ctx, "aura_authula_grants")

	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	bootstrap := roleDSN(t, "aura", "aura_authula_grants")

	super, err := Open(ctx, &Config{URL: bootstrap})
	if err != nil {
		t.Fatalf("open bootstrap: %v", err)
	}
	defer super.Close()

	// Mimic 0019: the schema exists and is owned by aura_migrate, while aura_app may
	// create inside it. This is the exact shape the live database is in.
	if _, err := super.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS authula AUTHORIZATION aura_migrate"); err != nil {
		t.Fatalf("create authula schema: %v", err)
	}
	if _, err := super.Exec(ctx, "GRANT CREATE, USAGE ON SCHEMA authula TO aura_app"); err != nil {
		t.Fatalf("grant authula to aura_app: %v", err)
	}

	app, err := Open(ctx, &Config{URL: appURL})
	if err != nil {
		t.Fatalf("open aura_app: %v", err)
	}
	defer app.Close()

	// A table that pre-dates the fix, as Authula's tables do on the live deployment.
	if _, err := app.Exec(ctx, "CREATE TABLE authula.existing_table (id int)"); err != nil {
		t.Fatalf("create pre-existing authula table: %v", err)
	}

	if err := EnsureRoles(ctx, bootstrap, pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}

	// A table Authula creates LATER — the case ALTER DEFAULT PRIVILEGES exists for.
	if _, err := app.Exec(ctx, "CREATE TABLE authula.future_table (id int)"); err != nil {
		t.Fatalf("create post-fix authula table: %v", err)
	}

	migrate, err := Open(ctx, &Config{URL: roleDSN(t, "aura_migrate", "aura_authula_grants")})
	if err != nil {
		t.Fatalf("open aura_migrate: %v", err)
	}
	defer migrate.Close()

	for _, table := range []string{"existing_table", "future_table"} {
		// SELECT is what pg_dump needs; it is also what grants the ACCESS SHARE lock the
		// failing LOCK TABLE was asking for, so this reproduces the backup's own access.
		var n int
		if err := migrate.QueryRow(ctx, "SELECT count(*) FROM authula."+table).Scan(&n); err != nil {
			t.Errorf("aura_migrate cannot read authula.%s — the nightly backup would abort "+
				"here exactly as it did on the live deployment: %v", table, err)
		}
		if _, err := migrate.Exec(ctx, "BEGIN; LOCK TABLE authula."+table+" IN ACCESS SHARE MODE; COMMIT"); err != nil {
			t.Errorf("aura_migrate cannot LOCK authula.%s, which is the statement pg_dump "+
				"issues and the one that failed: %v", table, err)
		}
	}
}

// TestEnsureAuthulaGrantsAreANoOpWithoutTheSchema pins the guard. EnsureRoles runs BEFORE
// Migrate, so on a first boot the authula schema does not exist yet; an unguarded GRANT
// would fail there and take the whole bootstrap down with it.
func TestEnsureAuthulaGrantsAreANoOpWithoutTheSchema(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, _, _ = fresh0093Database(t, ctx, "aura_authula_absent")

	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	if err := EnsureRoles(ctx, roleDSN(t, "aura", "aura_authula_absent"), pwd); err != nil {
		t.Fatalf("EnsureRoles must succeed when the authula schema is absent: %v", err)
	}
}

// roleDSN builds a connection string for one role against one database, using the same
// host/port defaults the other integration helpers use.
func roleDSN(t *testing.T, role, database string) string {
	t.Helper()
	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	host, port := os.Getenv("PGHOST"), os.Getenv("PGPORT")
	if host == "" {
		host = "127.0.0.1"
	}
	if port == "" {
		port = "5432"
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", role, pwd, host, port, database)
}
