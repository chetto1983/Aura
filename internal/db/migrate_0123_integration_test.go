//go:build db_integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestMigrate0123CapabilityDenialsFreshUpDownUp proves 0123's own round trip (02-04 Task
// 1, RBAC-09/RBAC-10): up creates aura.capability_denials with row-level security and the
// closed-set cause CHECK constraint; down drops it cleanly; up rebuilds it. Mirrors
// migrate_0094_integration_test.go's FreshUpDownUp shape — the ledger is derived evidence
// (02-04-PLAN reversibility rating: costly, not one-way), so a clean drop/rebuild is the
// right proof rather than a guarded down.
func TestMigrate0123CapabilityDenialsFreshUpDownUp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, migrateURL, _ := fresh0093Database(t, ctx, "aura_migrate0123_denials")

	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("migrate fresh database to head: %v", err)
	}
	headVersion := currentMigrationVersion(t, ctx, admin)
	if headVersion < 123 {
		t.Fatalf("full Migrate up reached version %d, want at least 123", headVersion)
	}
	assert0123Schema(t, ctx, admin)

	stepsToBefore0123, err := MigrationStepsAbove(122)
	if err != nil {
		t.Fatalf("MigrationStepsAbove(122): %v", err)
	}
	if err := MigrateSteps(ctx, migrateURL, -stepsToBefore0123); err != nil {
		t.Fatalf("MigrateSteps(%d) down to pre-0123: %v", -stepsToBefore0123, err)
	}
	if got := currentMigrationVersion(t, ctx, admin); got != 122 {
		t.Fatalf("version after down = %d, want 122", got)
	}
	if regclass(t, ctx, admin, "capability_denials") != nil {
		t.Fatal("aura.capability_denials survived the down migration")
	}

	if err := MigrateSteps(ctx, migrateURL, stepsToBefore0123); err != nil {
		t.Fatalf("migrate 0123 back up: %v", err)
	}
	assert0123Schema(t, ctx, admin)
}

// assert0123Schema pins the two properties the RBAC-09/RBAC-10 recorder depends on: the
// table exists with row-level security enabled, and an out-of-vocabulary cause is
// rejected by the CHECK constraint rather than silently becoming free text.
func assert0123Schema(t *testing.T, ctx context.Context, admin *pgxpool.Pool) {
	t.Helper()
	if regclass(t, ctx, admin, "capability_denials") == nil {
		t.Fatal("aura.capability_denials missing after migrate")
	}
	var rowSecurity bool
	if err := admin.QueryRow(ctx,
		`SELECT relrowsecurity FROM pg_class c
		 JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = 'aura' AND c.relname = 'capability_denials'`).Scan(&rowSecurity); err != nil {
		t.Fatalf("read rowsecurity for capability_denials: %v", err)
	}
	if !rowSecurity {
		t.Fatal("aura.capability_denials has no row-level security")
	}

	_, err := admin.Exec(ctx, `
INSERT INTO aura.capability_denials (identity_id, capability, route, cause)
VALUES ('(no-principal)', 'agent.run', 'POST /agent/run', 'not_a_real_cause')`)
	if err == nil {
		t.Fatal("an out-of-vocabulary cause must be rejected by the CHECK constraint")
	}
}
