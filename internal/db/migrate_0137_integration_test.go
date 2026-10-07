//go:build db_integration

package db

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var turnDecisionColumns = []string{
	"recall_context_key", "reasoning_effort", "reasoning_effort_requested", "reasoning_effort_source",
	"reasoning_effort_route_key", "reasoning_effort_policy_version", "reasoning_effort_origin_ref",
}

// 0137 adds the seven columns turn recall persists a decision in (spec 2026-10-06, "Persisting
// what was learned"). All nullable and never backfilled: a row without provenance can never
// become a reusable label.
func TestMigrate0137AddsTheTurnDecisionColumns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, migrateURL, _ := fresh0093Database(t, ctx, "aura_migrate0137_decision")
	migrateToVersion(t, ctx, migrateURL, admin, 136)
	for _, column := range turnDecisionColumns {
		if turnColumnExists(t, ctx, admin, column) {
			t.Fatalf("%s exists before 0137", column)
		}
	}

	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0137 up: %v", err)
	}
	for _, column := range turnDecisionColumns {
		if !turnColumnExists(t, ctx, admin, column) {
			t.Errorf("%s missing after 0137", column)
		}
	}
	var definition string
	if err := admin.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint
		WHERE conname = 'conversation_turns_reasoning_effort_source_check'`).Scan(&definition); err != nil {
		t.Fatalf("read the source CHECK: %v", err)
	}
	for _, source := range []string{"user", "teacher", "memory", "seeds", "greeting", "fallback"} {
		if !strings.Contains(definition, "'"+source+"'") {
			t.Errorf("source CHECK %q does not admit %q", definition, source)
		}
	}

	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("migrate 0137 down: %v", err)
	}
	for _, column := range turnDecisionColumns {
		if turnColumnExists(t, ctx, admin, column) {
			t.Errorf("%s survived 0137 down", column)
		}
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0137 up again: %v", err)
	}
}

func turnColumnExists(t *testing.T, ctx context.Context, admin *pgxpool.Pool, column string) bool {
	t.Helper()
	var exists bool
	if err := admin.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_schema = 'aura' AND table_name = 'conversation_turns' AND column_name = $1)`, column).Scan(&exists); err != nil {
		t.Fatalf("read column %s: %v", column, err)
	}
	return exists
}
