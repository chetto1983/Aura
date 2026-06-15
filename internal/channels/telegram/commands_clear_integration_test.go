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
	"testing"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
)

// TestClearHardDeletesPersistedConversationE2E is the live /clear contract: dispatch
// → clearBackend → conversations.Store.Delete → Postgres cascade. After /clear the
// conversation and all its turns are gone, and the same convID can be re-created
// fresh (the next inbound message's EnsureConversation path).
func TestClearHardDeletesPersistedConversationE2E(t *testing.T) {
	pool := migratedPool(t)
	ctx := context.Background()

	conv := conversations.New(pool, conversations.Config{RunDir: t.TempDir()})

	const chatID int64 = 9_100_000_001
	cid := convID(chatID)

	// Best-effort cleanup: if an assertion fails before /clear runs, don't leak the
	// row. The happy path deletes it, making this a no-op.
	t.Cleanup(func() { _ = conv.Delete(context.Background(), cid) })

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

	// The chat starts over: the SAME deterministic convID re-creates clean (the
	// EnsureConversation path the next inbound message takes), with zero turns.
	if _, err := conv.Create(ctx, conversations.CreateParams{ID: cid, IdentityID: localID, Model: "integration-test"}); err != nil {
		t.Fatalf("re-Create on the same convID after /clear: %v", err)
	}
	if n, err := conv.CountTurns(ctx, cid); err != nil || n != 0 {
		t.Fatalf("fresh conversation CountTurns = %d, err = %v; want 0, nil", n, err)
	}
}
