package runner

import (
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/conversations"
)

// TestEnsureConversation_CreatesThenIdempotent proves the lazy-create seam channels
// rely on: a chat whose conversation row does not exist yet is created on first
// call, and a second call is a no-op (no error, no duplicate, same id) — the bug
// the live Telegram inbound test surfaced (handleTurn drove Turn on a never-created
// conversation, so AppendTurn FK-failed and the user saw "❌ Errore").
func TestEnsureConversation_CreatesThenIdempotent(t *testing.T) {
	r, conv := newRunnerWithCacheFake(t, agenttest.NewFakeClient(), newFakeCacheMetricStore())
	const convID = "11111111-1111-1111-1111-111111111111"

	if _, err := conv.Get(t.Context(), convID); err == nil {
		t.Fatalf("precondition: conversation %s should not exist yet", convID)
	}

	if err := r.EnsureConversation(t.Context(), convID); err != nil {
		t.Fatalf("first EnsureConversation: %v", err)
	}
	got, err := conv.Get(t.Context(), convID)
	if err != nil {
		t.Fatalf("conversation not created: %v", err)
	}
	if got.ID != convID {
		t.Fatalf("created id = %q, want %q", got.ID, convID)
	}

	// Second call must be a no-op: idempotent, no error, still exactly one row.
	if err := r.EnsureConversation(t.Context(), convID); err != nil {
		t.Fatalf("second EnsureConversation (idempotent): %v", err)
	}
	all, err := conv.List(t.Context(), true)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if n := countWithID(all, convID); n != 1 {
		t.Fatalf("conversation row count for %s = %d, want 1 (idempotent)", convID, n)
	}
}

func countWithID(cs []conversations.Conversation, id string) int {
	n := 0
	for _, c := range cs {
		if c.ID == id {
			n++
		}
	}
	return n
}
