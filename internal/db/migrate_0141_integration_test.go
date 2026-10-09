//go:build db_integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var workBoardTables = []string{"boards", "board_cards", "board_views"}

// 0141 adds the work board (prd.md §16, 2026-10-09). The test pins what the store relies on:
// all three tables under the fail-closed RLS pair, the card CHECKs that keep the widget's
// vocabulary (priority 1-3, the three sources, the two editors), and a clean rollback.
func TestMigrate0141AddsTheWorkBoard(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, migrateURL, _ := fresh0093Database(t, ctx, "aura_migrate0141_board")
	migrateToVersion(t, ctx, migrateURL, admin, 140)
	for _, table := range workBoardTables {
		if regclass(t, ctx, admin, table) != nil {
			t.Fatalf("aura.%s exists before 0141", table)
		}
	}

	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0141 up: %v", err)
	}
	assert0141Schema(t, ctx, admin)

	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("migrate 0141 down: %v", err)
	}
	for _, table := range workBoardTables {
		if regclass(t, ctx, admin, table) != nil {
			t.Fatalf("aura.%s survived 0141 down", table)
		}
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0141 up again: %v", err)
	}
	assert0141Schema(t, ctx, admin)
}

func assert0141Schema(t *testing.T, ctx context.Context, admin *pgxpool.Pool) {
	t.Helper()
	for _, table := range workBoardTables {
		var rowSecurity bool
		if err := admin.QueryRow(ctx,
			`SELECT relrowsecurity FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
			 WHERE n.nspname = 'aura' AND c.relname = $1`, table).Scan(&rowSecurity); err != nil {
			t.Fatalf("read rowsecurity for %s: %v", table, err)
		}
		if !rowSecurity {
			t.Fatalf("aura.%s has no row-level security", table)
		}
		var restrictive int
		if err := admin.QueryRow(ctx,
			`SELECT count(*) FROM pg_policies
			 WHERE schemaname = 'aura' AND tablename = $1 AND permissive = 'RESTRICTIVE'`, table).Scan(&restrictive); err != nil {
			t.Fatalf("count restrictive policies on %s: %v", table, err)
		}
		if restrictive != 1 {
			t.Fatalf("aura.%s has %d restrictive policies, want 1", table, restrictive)
		}
	}

	var identityID, boardID string
	if err := admin.QueryRow(ctx, `SELECT id::text FROM aura.identities LIMIT 1`).Scan(&identityID); err != nil {
		t.Fatalf("read a seeded identity: %v", err)
	}
	if err := admin.QueryRow(ctx, `
INSERT INTO aura.boards (identity_id, name, columns) VALUES ($1, 'main', '[]'::jsonb)
ON CONFLICT (identity_id, name) DO UPDATE SET name = EXCLUDED.name
RETURNING id::text`, identityID).Scan(&boardID); err != nil {
		t.Fatalf("insert a board: %v", err)
	}
	insert := func(priority int, source, updatedBy string) error {
		_, err := admin.Exec(ctx, `
INSERT INTO aura.board_cards (board_id, identity_id, column_id, position, label, priority, source, updated_by)
VALUES ($1, $2, 'todo', 1, 'call the supplier', $3, $4, $5)`, boardID, identityID, priority, source, updatedBy)
		return err
	}
	if err := insert(2, "chat", "agent"); err != nil {
		t.Fatalf("a valid card was refused: %v", err)
	}
	for name, bad := range map[string]func() error{
		"priority 4":      func() error { return insert(4, "chat", "agent") },
		"unknown source":  func() error { return insert(2, "email", "agent") },
		"unknown updater": func() error { return insert(2, "chat", "robot") },
	} {
		if bad() == nil {
			t.Errorf("%s must be rejected by a CHECK constraint", name)
		}
	}
}
