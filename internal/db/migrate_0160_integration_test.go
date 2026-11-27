//go:build db_integration

package db

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const refuseResumeOfPendingToolApproval = "aura.refuse_resume_of_pending_tool_approval()"

// 0160 adds the durable record of a parked tool call, owner-scoped, and the guard that keeps a
// binary which no longer knows the record from resuming its pause while the record is pending.
func TestMigration0160CreatesToolApprovals(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin, migrateURL, _ := fresh0093Database(t, ctx, "aura_migrate0160_tool_approvals")
	migrateToVersion(t, ctx, migrateURL, admin, 159)
	assert0160Absent(t, ctx, admin)
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0160 up: %v", err)
	}
	assert0160Schema(t, ctx, admin)
	assert0160Checks(t, ctx, admin)

	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("migrate 0160 down: %v", err)
	}
	assert0160Absent(t, ctx, admin)
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0160 up again: %v", err)
	}
	assert0160Schema(t, ctx, admin)
}

func assert0160Schema(t *testing.T, ctx context.Context, admin *pgxpool.Pool) {
	t.Helper()
	var rowSecurity bool
	if err := admin.QueryRow(ctx, `SELECT relrowsecurity FROM pg_class WHERE oid = 'aura.tool_approvals'::regclass`).Scan(&rowSecurity); err != nil || !rowSecurity {
		t.Fatalf("aura.tool_approvals row security = %t, %v; want it on", rowSecurity, err)
	}
	rows, err := admin.Query(ctx, `
SELECT policyname || ' ' || permissive || ' ' || array_to_string(roles, ',') || ' ' || cmd
FROM pg_policies WHERE schemaname = 'aura' AND tablename = 'tool_approvals' ORDER BY policyname`)
	if err != nil {
		t.Fatalf("read the record's policies: %v", err)
	}
	policies, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("scan the record's policies: %v", err)
	}
	want := []string{
		"tool_approvals_owner_isolation PERMISSIVE public ALL",
		"tool_approvals_require_identity RESTRICTIVE aura_app ALL",
	}
	if !slices.Equal(policies, want) {
		t.Fatalf("policies = %q, want %q", policies, want)
	}

	var definer, publicCanRun bool
	var config []string
	var owner string
	if err := admin.QueryRow(ctx, `
SELECT p.prosecdef, coalesce(p.proconfig, '{}'), r.rolname, has_function_privilege('public', p.oid, 'EXECUTE')
FROM pg_proc p JOIN pg_roles r ON r.oid = p.proowner
WHERE p.oid = to_regprocedure($1)`, refuseResumeOfPendingToolApproval).Scan(&definer, &config, &owner, &publicCanRun); err != nil {
		t.Fatalf("read %s: %v", refuseResumeOfPendingToolApproval, err)
	}
	if !definer || owner != "aura_migrate" || !slices.Contains(config, "search_path=pg_catalog, aura") || publicCanRun {
		t.Fatalf("%s: definer=%t owner=%s config=%v public=%t; want a definer owned by aura_migrate, search_path pinned, PUBLIC revoked",
			refuseResumeOfPendingToolApproval, definer, owner, config, publicCanRun)
	}
	var triggers int
	if err := admin.QueryRow(ctx, `
SELECT count(*) FROM pg_trigger
WHERE tgrelid = 'aura.paused_states'::regclass AND NOT tgisinternal AND tgfoid = to_regprocedure($1)`,
		refuseResumeOfPendingToolApproval).Scan(&triggers); err != nil || triggers != 1 {
		t.Fatalf("paused_states triggers calling the guard = %d, %v; want 1", triggers, err)
	}
}

// assert0160Checks inserts as the migrate-owning admin, past RLS, so only the constraints refuse,
// each with its own SQLSTATE.
func assert0160Checks(t *testing.T, ctx context.Context, admin *pgxpool.Pool) {
	t.Helper()
	var identity string
	if err := admin.QueryRow(ctx, `SELECT id::text FROM aura.identities LIMIT 1`).Scan(&identity); err != nil {
		t.Fatal(err)
	}
	conv := uuid.NewString()
	if _, err := admin.Exec(ctx, `INSERT INTO aura.conversations (id, identity_id) VALUES ($1, $2)`, conv, identity); err != nil {
		t.Fatal(err)
	}
	insertInto := func(conversation string, index int, status, scope, args string, decided, started bool) error {
		_, err := admin.Exec(ctx, `INSERT INTO aura.tool_approvals
(identity_id, conversation_id, pause_token, assistant_seq, call_index, tool_call_id, tool_name, source,
 execute_args, execute_fingerprint, status, approval_scope, decided_at, started_at)
VALUES ($1, $2, $3, 4, $4, 'call', 'shell_exec', 'tool', $5::jsonb, repeat('a', 64), $6, NULLIF($7, ''),
 CASE WHEN $8 THEN now() END, CASE WHEN $9 THEN now() END)`,
			identity, conversation, uuid.NewString(), index, args, status, scope, decided, started)
		return err
	}
	insert := func(index int, status, scope, args string, decided, started bool) error {
		return insertInto(conv, index, status, scope, args, decided, started)
	}
	if err := insert(0, "pending", "", `{}`, false, false); err != nil {
		t.Fatalf("a pending record was refused: %v", err)
	}
	if err := insert(1, "running", "session", `{}`, true, true); err != nil {
		t.Fatalf("a running record was refused: %v", err)
	}
	const check, unique, foreignKey = "23514", "23505", "23503"
	for name, bad := range map[string]struct {
		insert func() error
		code   string
	}{
		"a negative call index":       {func() error { return insert(-1, "pending", "", `{}`, false, false) }, check},
		"an unknown status":           {func() error { return insert(2, "approved", "", `{}`, true, false) }, check},
		"an unknown scope":            {func() error { return insert(3, "accepted", "forever", `{}`, true, false) }, check},
		"non-object arguments":        {func() error { return insert(4, "pending", "", `[]`, false, false) }, check},
		"a decided record, undated":   {func() error { return insert(5, "declined", "", `{}`, false, false) }, check},
		"a pending record, dated":     {func() error { return insert(6, "pending", "", `{}`, true, false) }, check},
		"a running record, unstarted": {func() error { return insert(7, "running", "", `{}`, true, false) }, check},
		"the same slot twice":         {func() error { return insert(0, "pending", "", `{}`, false, false) }, unique},
		"an unknown conversation": {func() error {
			return insertInto(uuid.NewString(), 0, "pending", "", `{}`, false, false)
		}, foreignKey},
	} {
		var pgErr *pgconn.PgError
		if err := bad.insert(); !errors.As(err, &pgErr) || pgErr.Code != bad.code {
			t.Errorf("%s: err = %v, want SQLSTATE %s", name, err, bad.code)
		}
	}
}

func assert0160Absent(t *testing.T, ctx context.Context, admin *pgxpool.Pool) {
	t.Helper()
	if regclass(t, ctx, admin, "tool_approvals") != nil {
		t.Fatal("aura.tool_approvals exists without 0160")
	}
	var function *string
	if err := admin.QueryRow(ctx, `SELECT to_regprocedure($1)::text`, refuseResumeOfPendingToolApproval).Scan(&function); err != nil || function != nil {
		t.Fatalf("the guard function without 0160 = %v, %v; want none", function, err)
	}
}
