//go:build db_integration

// Integration E2E for the /clear command's data path. It drives the REAL command
// dispatcher (commands.dispatch) with the Clear seam wired to a live
// conversations.Store, against a migrated Postgres, and proves the end-to-end
// contract a unit test with a double cannot: a /clear on a persisted conversation
// hard-deletes the row AND cascades its turns away (the migration FKs ON DELETE
// CASCADE), and a fresh conversation can then be created on the SAME deterministic
// convID — i.e. the chat genuinely "starts over".
//
// Run via:
//
//	go test -tags db_integration -race ./internal/channels/telegram
//
// migratedPool / localID are shared with store_integration_test.go (same package).
package telegram

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
)

// TestClearHardDeletesPersistedConversationE2E is the live /clear contract: dispatch
// → clearBackend → conversations.Store.Delete → Postgres cascade. After /clear the
// conversation and all its turns are gone, and the same convID can be re-created
// fresh (the next inbound message's EnsureConversation path).
func TestClearHardDeletesPersistedConversationE2E(t *testing.T) {
	pool := migratedPool(t)
	ctx := ownerCtx()

	conv := conversations.New(pool, conversations.Config{RunDir: t.TempDir()})

	const chatID int64 = 9_100_000_001
	cid := convID(chatID)

	// Best-effort cleanup: if an assertion fails before /clear runs, don't leak the
	// row. The happy path deletes it, making this a no-op.
	t.Cleanup(func() { _ = conv.Delete(ownerCtx(), cid) })

	if _, err := conv.Create(ctx, conversations.CreateParams{ID: cid, IdentityID: localID, Model: "integration-test"}); err != nil {
		t.Fatalf("seed Create conversation: %v", err)
	}
	seed := []conversations.AppendTurnParams{
		{ConversationID: cid, Role: llm.RoleUser, Content: "ciao Aura"},
		{ConversationID: cid, Role: llm.RoleAssistant, Content: "ciao! come posso aiutarti?"},
	}
	for _, p := range seed {
		if err := conv.AppendTurn(ctx, p); err != nil {
			t.Fatalf("seed AppendTurn %q: %v", p.Content, err)
		}
	}

	// Precondition: the conversation exists with its turns persisted.
	if n, err := conv.CountTurns(ctx, cid); err != nil || n != 2 {
		t.Fatalf("precondition CountTurns = %d, err = %v; want 2, nil", n, err)
	}

	// Seed an append-only tool_invocations row — the realistic case: ANY conversation
	// in which the agent used a tool has these. This is the row whose ON DELETE CASCADE
	// teardown the 0011 reject-all trigger wrongly blocked, breaking /clear. The earlier
	// turns-only fixture never exercised it, so the bug shipped green (regression guard
	// for migration 0016).
	seedToolInvocation(t, ctx, pool, cid)

	// Drive the REAL dispatcher with the live store wired as the Clear backend.
	cmds := newCommands(commandDeps{Clear: conv})
	handled, reply := cmds.dispatch(ctx, chatID, "/clear")
	if !handled {
		t.Fatal("/clear must be handled (bot-intercept, never the LLM)")
	}
	if reply == "" {
		t.Error("/clear should confirm the wipe to the user")
	}

	// The conversation row is gone (cascade wiped its turns with it).
	if _, err := conv.Get(ctx, cid); !errors.Is(err, conversations.ErrConversationNotFound) {
		t.Fatalf("after /clear: Get must report not-found, got err = %v", err)
	}

	// The ON DELETE CASCADE also tore down the append-only tool_invocations rows — the
	// path 0016 unblocks. Before the fix, this delete aborted here and /clear failed.
	if n := countToolInvocations(t, ctx, pool, cid); n != 0 {
		t.Fatalf("after /clear: tool_invocations rows = %d; want 0 (cascade)", n)
	}

	// The chat starts over: the SAME deterministic convID re-creates clean (the
	// EnsureConversation path the next inbound message takes), with zero turns.
	if _, err := conv.Create(ctx, conversations.CreateParams{ID: cid, IdentityID: localID, Model: "integration-test"}); err != nil {
		t.Fatalf("re-Create on the same convID after /clear: %v", err)
	}
	if n, err := conv.CountTurns(ctx, cid); err != nil || n != 0 {
		t.Fatalf("fresh conversation CountTurns = %d, err = %v; want 0, nil", n, err)
	}
}

// TestToolInvocationsImmutableExceptConversationCascade locks the migration 0016
// contract at the DB level: a standalone DELETE on a tool_invocations row whose
// conversation is still alive is rejected (append-only anti-tampering preserved),
// while deleting the parent conversation cascades the rows away (the path /clear and
// `aura chat delete` depend on).
func TestToolInvocationsImmutableExceptConversationCascade(t *testing.T) {
	pool := migratedPool(t)
	ctx := ownerCtx()
	conv := conversations.New(pool, conversations.Config{RunDir: t.TempDir()})

	const chatID int64 = 9_100_000_002
	cid := convID(chatID)
	t.Cleanup(func() { _ = conv.Delete(ownerCtx(), cid) })

	if _, err := conv.Create(ctx, conversations.CreateParams{ID: cid, IdentityID: localID, Model: "integration-test"}); err != nil {
		t.Fatalf("seed Create conversation: %v", err)
	}
	seedToolInvocation(t, ctx, pool, cid)

	// Anti-tampering: a direct DELETE while the conversation is alive must be rejected
	// (the append-only trigger; aura_app additionally lacks the DELETE grant) and must
	// leave the row intact.
	if _, err := pool.Exec(ctx,
		`DELETE FROM aura.tool_invocations WHERE conversation_id = $1::uuid`, cid); err == nil {
		t.Fatal("standalone DELETE on a live conversation's tool_invocations must be rejected")
	} else if !strings.Contains(err.Error(), "tool_invocations") {
		t.Fatalf("rejected for an unexpected reason: %v", err)
	}
	if n := countToolInvocations(t, ctx, pool, cid); n != 1 {
		t.Fatalf("rejected DELETE must leave the row intact; got %d rows", n)
	}

	// Cascade: deleting the parent conversation tears the row down (the 0016 fix).
	if err := conv.Delete(ctx, cid); err != nil {
		t.Fatalf("conversation Delete (cascade) must succeed: %v", err)
	}
	if n := countToolInvocations(t, ctx, pool, cid); n != 0 {
		t.Fatalf("cascade must remove tool_invocations; got %d rows", n)
	}
}

// seedToolInvocation inserts one append-only 'start' row for cid — the realistic
// fixture (any tool-using conversation has these) whose cascade teardown 0016 unblocks.
func seedToolInvocation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, cid string) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.tool_invocations
			(conversation_id, request_id, tool_call_id, tool_name, event_kind, seq, ts, started_at)
		 VALUES ($1::uuid, gen_random_uuid(), 'call-1', 'shell_exec', 'start', 1, now(), now())`,
		cid); err != nil {
		t.Fatalf("seed tool_invocations: %v", err)
	}
}

// countToolInvocations returns the number of ledger rows for cid.
func countToolInvocations(t *testing.T, ctx context.Context, pool *pgxpool.Pool, cid string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM aura.tool_invocations WHERE conversation_id = $1::uuid`, cid).Scan(&n); err != nil {
		t.Fatalf("count tool_invocations: %v", err)
	}
	return n
}
