//go:build db_integration

package dbtest

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// DrillDatabase creates the throwaway database name for a test that migrates a schema of its
// own. adminURL is a DSN for a role allowed to CREATE DATABASE, on any existing database. A
// leftover from an earlier run is dropped first, and aura_migrate gets CREATE on the database
// and on its public schema, which a migration from zero needs.
//
// The drop at the end runs on a connection of its own and fails the test when it fails. The
// tests this replaces dropped through a pool their own defer had already closed -- a defer
// runs when the test function returns, before t.Cleanup -- and ignored the error, so every
// drill database outlived its run: 14 on a developer's Postgres after one run of the
// db_integration tier, measured 2026-10-04. CI never noticed because its container is
// thrown away.
func DrillDatabase(t testing.TB, adminURL, name string) {
	t.Helper()
	ident := pgx.Identifier{name}.Sanitize()
	exec(t, adminURL, "DROP DATABASE IF EXISTS "+ident+" WITH (FORCE)")
	exec(t, adminURL, "CREATE DATABASE "+ident)
	t.Cleanup(func() {
		if err := execErr(adminURL, "DROP DATABASE IF EXISTS "+ident+" WITH (FORCE)"); err != nil {
			t.Errorf("drop drill database %s: %v", name, err)
		}
	})
	exec(t, adminURL, "GRANT CREATE ON DATABASE "+ident+" TO aura_migrate")
	exec(t, withDatabase(t, adminURL, name), "GRANT CREATE ON SCHEMA public TO aura_migrate")
}

func exec(t testing.TB, dsn, sql string) {
	t.Helper()
	if err := execErr(dsn, sql); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func execErr(dsn, sql string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.Background()) }()
	_, err = conn.Exec(ctx, sql)
	return err
}

// withDatabase is dsn pointed at database instead of its own.
func withDatabase(t testing.TB, dsn, database string) string {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse admin DSN: %v", err)
	}
	parsed.Path = "/" + database
	return parsed.String()
}
