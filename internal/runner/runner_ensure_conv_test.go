package runner

import (
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/jackc/pgx/v5/pgconn"
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

// TestEnsureConversation_SwallowsOnlyUniqueViolation proves M-08: the lost-race
// reconciliation swallows ONLY the benign 23505 unique_violation (a concurrent
// first-message creator won), and surfaces every other create failure instead of
// masking it as "already exists". A 23503 (or any non-23505) error that left a row
// present must NOT be hidden — that would mask a real create failure.
func TestEnsureConversation_SwallowsOnlyUniqueViolation(t *testing.T) {
	const convID = "22222222-2222-2222-2222-222222222222"

	t.Run("23505 unique_violation is swallowed (benign concurrent-create race)", func(t *testing.T) {
		r, conv := newRunnerWithCacheFake(t, agenttest.NewFakeClient(), newFakeCacheMetricStore())
		conv.createEr = &pgconn.PgError{Code: "23505"}
		conv.createRow = true // a concurrent creator won: the row exists despite our error
		if err := r.EnsureConversation(t.Context(), convID); err != nil {
			t.Fatalf("23505 should be swallowed as the benign race, got: %v", err)
		}
	})

	t.Run("non-23505 create failure is surfaced, not masked", func(t *testing.T) {
		r, conv := newRunnerWithCacheFake(t, agenttest.NewFakeClient(), newFakeCacheMetricStore())
		realErr := &pgconn.PgError{Code: "23503"} // FK violation: a real failure
		conv.createEr = realErr
		conv.createRow = true // even if a row is present, a non-23505 error must not be hidden
		err := r.EnsureConversation(t.Context(), convID)
		if err == nil {
			t.Fatal("non-23505 create failure was masked as success — real failure swallowed")
		}
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
			t.Fatalf("EnsureConversation should surface the original 23503 error, got: %v", err)
		}
	})

	t.Run("plain (non-pg) create failure is surfaced", func(t *testing.T) {
		r, conv := newRunnerWithCacheFake(t, agenttest.NewFakeClient(), newFakeCacheMetricStore())
		conv.createEr = errors.New("boom: connection refused")
		conv.createRow = true
		if err := r.EnsureConversation(t.Context(), convID); err == nil {
			t.Fatal("plain create failure was masked as success — real failure swallowed")
		}
	})
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
