//go:build db_integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The ledger's rollback carries no guard clause, unlike 0093's: the rows are derived,
// passive and reconstructible by working again. This asserts exactly that -- down drops
// both tables cleanly and up rebuilds them -- rather than trusting a migration nobody
// ran backwards.
func TestMigrate0094FreshUpDownUp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, migrateURL, _ := fresh0093Database(t, ctx, "aura_migrate0094_roundtrip")

	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("migrate fresh database to 0094: %v", err)
	}
	assert0094Schema(t, ctx, admin)

	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("migrate 0094 down to 0093: %v", err)
	}
	if got := currentMigrationVersion(t, ctx, admin); got != 93 {
		t.Fatalf("version after down = %d, want 93", got)
	}
	for _, table := range []string{"verification_events", "verification_state"} {
		if regclass(t, ctx, admin, table) != nil {
			t.Fatalf("%s survived the down migration", table)
		}
	}

	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0094 up again: %v", err)
	}
	assert0094Schema(t, ctx, admin)
}

// assert0094Schema pins the three properties the policy depends on: both tables exist,
// both carry row-level security, and the scope check rejects a value outside the two
// the classifier can produce. The scope constraint is the one worth a test -- it is what
// stops a single-file test run being read back as evidence the repo is green.
func assert0094Schema(t *testing.T, ctx context.Context, admin *pgxpool.Pool) {
	t.Helper()
	for _, table := range []string{"verification_events", "verification_state"} {
		if regclass(t, ctx, admin, table) == nil {
			t.Fatalf("%s missing after migrate", table)
		}
		var rowSecurity bool
		if err := admin.QueryRow(ctx,
			`SELECT relrowsecurity FROM pg_class c
			 JOIN pg_namespace n ON n.oid = c.relnamespace
			 WHERE n.nspname = 'aura' AND c.relname = $1`, table).Scan(&rowSecurity); err != nil {
			t.Fatalf("read rowsecurity for %s: %v", table, err)
		}
		if !rowSecurity {
			t.Fatalf("%s has no row-level security", table)
		}
	}

	var identity string
	if err := admin.QueryRow(ctx, `SELECT id::text FROM aura.identities LIMIT 1`).Scan(&identity); err != nil {
		t.Skipf("no identity seeded in the fresh database: %v", err)
	}
	_, err := admin.Exec(ctx, `
INSERT INTO aura.verification_events (
    identity_id, session_id, cwd, root, command, canonical_command, kind, scope, status, exit_code
) VALUES ($1, 's1', '/w', '/w', 'go test ./...', 'go test', 'test', 'whole-repo', 'passed', 0)`, identity)
	if err == nil {
		t.Fatal("an out-of-vocabulary scope must be rejected by the check constraint")
	}
}

func regclass(t *testing.T, ctx context.Context, admin *pgxpool.Pool, table string) *string {
	t.Helper()
	var name *string
	if err := admin.QueryRow(ctx, `SELECT to_regclass('aura.'||$1)::text`, table).Scan(&name); err != nil {
		t.Fatalf("to_regclass(%s): %v", table, err)
	}
	return name
}
