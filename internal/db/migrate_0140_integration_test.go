//go:build db_integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 0140 adds the per-identity tool policies beside the approval grants (prd.md §5,
// 2026-10-09). The test pins what the gateway relies on: the two-word vocabulary is a CHECK,
// not free text; the table is under the fail-closed RLS pair; and a rollback removes it.
func TestMigrate0140AddsTheToolPolicies(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, migrateURL, _ := fresh0093Database(t, ctx, "aura_migrate0140_policies")
	migrateToVersion(t, ctx, migrateURL, admin, 139)
	if regclass(t, ctx, admin, "gateway_tool_policies") != nil {
		t.Fatal("aura.gateway_tool_policies exists before 0140")
	}

	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0140 up: %v", err)
	}
	assert0140Schema(t, ctx, admin)

	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("migrate 0140 down: %v", err)
	}
	if regclass(t, ctx, admin, "gateway_tool_policies") != nil {
		t.Fatal("aura.gateway_tool_policies survived 0140 down")
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0140 up again: %v", err)
	}
	assert0140Schema(t, ctx, admin)
}

func assert0140Schema(t *testing.T, ctx context.Context, admin *pgxpool.Pool) {
	t.Helper()
	if regclass(t, ctx, admin, "gateway_tool_policies") == nil {
		t.Fatal("aura.gateway_tool_policies missing after migrate")
	}
	var rowSecurity bool
	if err := admin.QueryRow(ctx,
		`SELECT relrowsecurity FROM pg_class c
		 JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = 'aura' AND c.relname = 'gateway_tool_policies'`).Scan(&rowSecurity); err != nil {
		t.Fatalf("read rowsecurity: %v", err)
	}
	if !rowSecurity {
		t.Fatal("aura.gateway_tool_policies has no row-level security")
	}
	var restrictive int
	if err := admin.QueryRow(ctx,
		`SELECT count(*) FROM pg_policies
		 WHERE schemaname = 'aura' AND tablename = 'gateway_tool_policies' AND permissive = 'RESTRICTIVE'`).Scan(&restrictive); err != nil {
		t.Fatalf("count restrictive policies: %v", err)
	}
	if restrictive != 1 {
		t.Fatalf("restrictive policies = %d, want 1 (the 0087 require-identity pair)", restrictive)
	}

	var identityID string
	if err := admin.QueryRow(ctx, `SELECT id::text FROM aura.identities LIMIT 1`).Scan(&identityID); err != nil {
		t.Fatalf("read a seeded identity: %v", err)
	}
	if _, err := admin.Exec(ctx, `
INSERT INTO aura.gateway_tool_policies (identity_id, tool, action, policy)
VALUES ($1, 'calendar', 'send_email', 'maybe')`, identityID); err == nil {
		t.Fatal("a policy outside ask|deny must be rejected by the CHECK constraint")
	}
	if _, err := admin.Exec(ctx, `
INSERT INTO aura.gateway_tool_policies (identity_id, tool, action, policy)
VALUES ($1, 'calendar', 'send_email', 'deny')
ON CONFLICT DO NOTHING`, identityID); err != nil {
		t.Fatalf("insert a deny policy as admin: %v", err)
	}
	var visible int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM aura.gateway_tool_policies`).Scan(&visible); err != nil {
		t.Fatalf("count as admin: %v", err)
	}
	if visible != 1 {
		t.Fatalf("admin sees %d policies, want 1", visible)
	}
}
