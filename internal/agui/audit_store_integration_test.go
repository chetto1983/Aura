//go:build db_integration

// audit_store_integration_test.go proves the D-28 admin audit UNION read against a LIVE
// Postgres (the raw SQL in audit_store.go is not exercisable by the unit tier). It seeds
// the three identity-keyed ledgers (mcp_audit, skill_audit, tool_invocations via a
// conversation) for two fresh identities and asserts ListActivityForIdentity returns ONLY
// the queried identity's rows, newest-first across all three planes (cross-identity deny —
// the core MUSR-01 isolation property for the audit surface).
//
// No-skip-as-green: migratedPool/envOrSkip t.Fatal under $CI when the DSN is unset. The
// three ledgers are append-only (UPDATE/DELETE rejected by trigger), so the test uses fresh
// per-run identity UUIDs instead of cleaning up — each run asserts only its own rows.

package agui

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// seedConversationForIdentity inserts a conversation owned by the given identity and returns
// its id (the seedConversationRow sibling, parameterized on identity_id for the two-identity
// isolation proof). The append-only tool_invocations FK blocks cleanup, so the caller relies
// on fresh per-run identities rather than deleting.
func seedConversationForIdentity(t *testing.T, pool *pgxpool.Pool, identityID string) string {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	if _, err := pool.Exec(context.Background(),
		"INSERT INTO aura.conversations (id, identity_id, model) VALUES ($1, $2, 'test-model')",
		id, identityID); err != nil {
		t.Fatalf("insert conversation for %s: %v", identityID, err)
	}
	return id
}

// TestPgAuditStoreListActivityForIdentity is the live cross-identity isolation + ordering
// proof for the admin audit read.
func TestPgAuditStoreListActivityForIdentity(t *testing.T) {
	pool := migratedPool(t)
	ctx := context.Background()

	idA := uuid.Must(uuid.NewV7()).String()
	idB := uuid.Must(uuid.NewV7()).String()
	for _, id := range []string{idA, idB} {
		if _, err := pool.Exec(ctx,
			"INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')",
			id, "audit-"+id); err != nil {
			t.Fatalf("seed identity %s: %v", id, err)
		}
	}

	base := time.Now().UTC().Truncate(time.Second)
	tMCP := base.Add(-1 * time.Minute)
	tSkill := base.Add(-2 * time.Minute)
	tTool := base.Add(-3 * time.Minute)

	// A: one row on each of the three planes, at descending timestamps.
	if _, err := pool.Exec(ctx,
		"INSERT INTO aura.mcp_audit (actor_identity_id, action, server_name, reason, created_at) VALUES ($1,'install','srv-A','because',$2)",
		idA, tMCP); err != nil {
		t.Fatalf("seed mcp_audit A: %v", err)
	}
	if _, err := pool.Exec(ctx,
		"INSERT INTO aura.skill_audit (actor_id, identity_id, skill_name, action, content_hash, gate_recommended, gate_taken, created_at) VALUES ('model',$1,'skill-A','create','h', true, false, $2)",
		idA, tSkill); err != nil {
		t.Fatalf("seed skill_audit A: %v", err)
	}
	convA := seedConversationForIdentity(t, pool, idA)
	if _, err := pool.Exec(ctx,
		"INSERT INTO aura.tool_invocations (conversation_id, request_id, tool_call_id, tool_name, event_kind, seq, ts, started_at) VALUES ($1,$2,'tc-A','shell_exec','start',1,$3,$3)",
		convA, uuid.Must(uuid.NewV7()).String(), tTool); err != nil {
		t.Fatalf("seed tool_invocations A: %v", err)
	}

	// B: rows on the same planes that must NEVER appear in A's feed (cross-identity deny).
	if _, err := pool.Exec(ctx,
		"INSERT INTO aura.mcp_audit (actor_identity_id, action, server_name, created_at) VALUES ($1,'remove','srv-B',$2)",
		idB, base); err != nil {
		t.Fatalf("seed mcp_audit B: %v", err)
	}
	convB := seedConversationForIdentity(t, pool, idB)
	if _, err := pool.Exec(ctx,
		"INSERT INTO aura.tool_invocations (conversation_id, request_id, tool_call_id, tool_name, event_kind, seq, ts, started_at) VALUES ($1,$2,'tc-B','web_fetch','start',1,$3,$3)",
		convB, uuid.Must(uuid.NewV7()).String(), base); err != nil {
		t.Fatalf("seed tool_invocations B: %v", err)
	}

	store := NewPgAuditStore(pool)
	events, err := store.ListActivityForIdentity(ctx, idA, 50, 0)
	if err != nil {
		t.Fatalf("ListActivityForIdentity: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("events = %d, want 3 (mcp+skill+tool for A only)", len(events))
	}
	// Newest-first across the three planes: mcp (-1m) > skill (-2m) > tool (-3m).
	wantOrder := []struct{ source, target string }{
		{"mcp", "srv-A"},
		{"skill", "skill-A"},
		{"tool", "shell_exec"},
	}
	for i, want := range wantOrder {
		if events[i].Source != want.source || events[i].Target != want.target {
			t.Fatalf("events[%d] = (%s,%s), want (%s,%s)", i, events[i].Source, events[i].Target, want.source, want.target)
		}
	}
	for _, e := range events {
		if e.Target == "srv-B" || e.Target == "web_fetch" {
			t.Fatalf("cross-identity leak: identity B's %q appeared in A's feed", e.Target)
		}
	}

	// B's feed sees only B's two rows (mcp + tool), never A's.
	bEvents, err := store.ListActivityForIdentity(ctx, idB, 50, 0)
	if err != nil {
		t.Fatalf("ListActivityForIdentity B: %v", err)
	}
	if len(bEvents) != 2 {
		t.Fatalf("B events = %d, want 2", len(bEvents))
	}
}
