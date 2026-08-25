//go:build db_integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func migrateTo0101(t *testing.T, ctx context.Context, migrateURL string, admin *pgxpool.Pool) {
	t.Helper()
	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("migrate fresh database to head: %v", err)
	}
	for step := 0; step <= 32; step++ {
		switch version := currentMigrationVersion(t, ctx, admin); {
		case version == 101:
			return
		case version < 101:
			t.Fatalf("stepped past 0101: landed on version %d", version)
		default:
			if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
				t.Fatalf("step down toward 0101 from version %d: %v", version, err)
			}
		}
	}
	t.Fatalf("did not reach version 101 within 32 steps (now %d)", currentMigrationVersion(t, ctx, admin))
}

// TestMigrate0102BackfillsDecisionPolicyRoundTrip executes the shipped SQL against a
// real disposable PostgreSQL database. It starts at the exact pre-policy schema, seeds
// every resume_context shape already accepted by PostgreSQL, upgrades, rolls back, and
// upgrades again. This catches malformed SQL and lossy object handling that a catalog
// or file-presence assertion cannot see.
func TestMigrate0102BackfillsDecisionPolicyRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, migrateURL, _ := fresh0093Database(t, ctx, "aura_migrate0102_policy")
	migrateTo0101(t, ctx, migrateURL, admin)

	var convID string
	if err := admin.QueryRow(ctx, `
INSERT INTO aura.conversations (id, identity_id, model, status)
VALUES (gen_random_uuid(), $1::uuid, 'test-model', 'active')
RETURNING id::text`, seededOperatorIdentity).Scan(&convID); err != nil {
		t.Fatalf("seed conversation: %v", err)
	}
	if _, err := admin.Exec(ctx, `
INSERT INTO aura.paused_states (
    token, conversation_id, kind, question, resume_context, tool_call_id
) VALUES
    (gen_random_uuid(), $1::uuid, 'approval', 'null context', NULL, 'call-null'),
    (gen_random_uuid(), $1::uuid, 'approval', 'object context',
        '{"type":"scheduled_task_approval","task_id":"task-7"}'::jsonb, 'call-object'),
    (gen_random_uuid(), $1::uuid, 'approval', 'scalar context',
        '"scalar"'::jsonb, 'call-scalar')`, convID); err != nil {
		t.Fatalf("seed paused states at version 101: %v", err)
	}

	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0102 up: %v", err)
	}
	assert0102DecisionPolicies(t, ctx, admin, convID)
	assert0102ObjectFields(t, ctx, admin, convID)

	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("migrate 0102 down: %v", err)
	}
	if got := currentMigrationVersion(t, ctx, admin); got != 101 {
		t.Fatalf("version after down = %d, want 101", got)
	}
	var rowsWithPolicy int
	if err := admin.QueryRow(ctx, `
SELECT count(*) FROM aura.paused_states
WHERE conversation_id = $1::uuid AND resume_context ? 'allowed_decisions'`, convID).Scan(&rowsWithPolicy); err != nil {
		t.Fatalf("read contexts after down: %v", err)
	}
	if rowsWithPolicy != 0 {
		t.Fatalf("down migration left allowed_decisions on %d rows", rowsWithPolicy)
	}
	assert0102ObjectFields(t, ctx, admin, convID)

	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0102 up again: %v", err)
	}
	assert0102DecisionPolicies(t, ctx, admin, convID)
	assert0102ObjectFields(t, ctx, admin, convID)
}

func assert0102DecisionPolicies(t *testing.T, ctx context.Context, admin *pgxpool.Pool, convID string) {
	t.Helper()
	if got := currentMigrationVersion(t, ctx, admin); got != 102 {
		t.Fatalf("migration version = %d, want 102", got)
	}
	var validRows int
	if err := admin.QueryRow(ctx, `
SELECT count(*) FROM aura.paused_states
WHERE conversation_id = $1::uuid
  AND jsonb_typeof(resume_context) = 'object'
  AND resume_context->'allowed_decisions' = '["accept","decline","cancel"]'::jsonb`, convID).Scan(&validRows); err != nil {
		t.Fatalf("read migrated decision policies: %v", err)
	}
	if validRows != 3 {
		t.Fatalf("rows with explicit decision policy = %d, want 3", validRows)
	}
}

func assert0102ObjectFields(t *testing.T, ctx context.Context, admin *pgxpool.Pool, convID string) {
	t.Helper()
	var contextType, taskID string
	if err := admin.QueryRow(ctx, `
SELECT resume_context->>'type', resume_context->>'task_id'
FROM aura.paused_states
WHERE conversation_id = $1::uuid AND tool_call_id = 'call-object'`, convID).Scan(&contextType, &taskID); err != nil {
		t.Fatalf("read preserved object context: %v", err)
	}
	if contextType != "scheduled_task_approval" || taskID != "task-7" {
		t.Fatalf("object context after migration = {%q,%q}, want {scheduled_task_approval,task-7}", contextType, taskID)
	}
}
