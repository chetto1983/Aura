//go:build db_integration

// Integration tests for the cross-thread pending read (Store.ListPendingAll,
// APRV-01 / D-04). Split from store_test.go on touch (CLAUDE.md §Behavioral rules:
// the 600-LOC cap). Shares the migratedPool / newConversation / insertPause helpers
// declared in store_test.go (same package, same build tag).
//
//	go test -tags db_integration -race ./internal/askuser -run TestListPendingAll -count=10
package askuser

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// TestListPendingAll_CrossThread asserts ListPendingAll aggregates still-pending
// pauses across MULTIPLE conversations (the per-conversation ListPending cannot),
// excludes resumed rows, and carries the ProxiedFromChildID source (APRV-01 / D-04).
func TestListPendingAll_CrossThread(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	ctx := context.Background()

	convA := newConversation(t, pool)
	convB := newConversation(t, pool)

	tokA := insertPause(t, s, convA, "approval", "from A", 5)
	tokB := insertPause(t, s, convB, "clarification", "from B", 5)

	// A proxied pause carries the flat worker source id; a resumed pause must be excluded.
	childID := "w3"
	proxiedTok := uuid.Must(uuid.NewV7()).String()
	if err := s.Insert(ctx, InsertParams{
		Token:              proxiedTok,
		ConversationID:     convA,
		Kind:               "approval",
		Question:           "proxied from a worker",
		Priority:           5,
		ToolCallID:         "tc-proxied",
		ProxiedFromChildID: &childID,
	}); err != nil {
		t.Fatalf("Insert(proxied): %v", err)
	}
	resumedTok := insertPause(t, s, convB, "approval", "already answered", 5)
	if err := s.MarkResumed(ctx, resumedTok, ResumeAnswer{Action: ActionAccept, Content: "ok"}); err != nil {
		t.Fatalf("MarkResumed(seed resolved): %v", err)
	}

	pending, err := s.ListPendingAll(ctx, 100)
	if err != nil {
		t.Fatalf("ListPendingAll: %v", err)
	}

	got := map[string]Pending{}
	for _, p := range pending {
		got[p.Token] = p
	}
	// The two distinct-conversation pendings + the proxied pending are present;
	// the resumed pending is absent (resumed_at IS NULL filter).
	if _, ok := got[tokA]; !ok {
		t.Errorf("cross-thread list missing pending %s from conversation A", tokA)
	}
	if _, ok := got[tokB]; !ok {
		t.Errorf("cross-thread list missing pending %s from conversation B", tokB)
	}
	if _, ok := got[resumedTok]; ok {
		t.Errorf("cross-thread list leaked the resumed pending %s (resumed_at filter broken)", resumedTok)
	}
	// The two conversations both appear → genuinely cross-thread, not single-conversation.
	if got[tokA].ConversationID == got[tokB].ConversationID {
		t.Errorf("ListPendingAll did not aggregate across conversations: A=%s B=%s", got[tokA].ConversationID, got[tokB].ConversationID)
	}
	// The proxied pending exposes the source worker id (APRV-01 source thread).
	if p := got[proxiedTok]; p.ProxiedFromChildID != childID {
		t.Errorf("proxied pending ProxiedFromChildID = %q, want %q (APRV-01 source)", p.ProxiedFromChildID, childID)
	}
	// A direct (non-proxied) pending leaves the source empty.
	if p := got[tokA]; p.ProxiedFromChildID != "" {
		t.Errorf("direct pending ProxiedFromChildID = %q, want empty", p.ProxiedFromChildID)
	}
}

// TestListPendingAll_TotalOrderViaTokenTiebreaker: equal-priority rows inserted in
// ONE tx across DIFFERENT conversations share created_at = now() (the transaction
// timestamp), so the token ASC tiebreaker decides the cross-thread order
// deterministically (RESEARCH Pitfall 4). Inserted in non-sorted token order so a
// missing tiebreaker (insertion-order leak) is caught. Run under -count=10.
func TestListPendingAll_TotalOrderViaTokenTiebreaker(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	ctx := context.Background()
	convA := newConversation(t, pool)
	convB := newConversation(t, pool)

	// Three equal-priority pendings, spread across two conversations, in one tx so
	// created_at ties and only the token ASC tiebreaker orders them.
	rows := []struct {
		token, conv string
	}{
		{"cccccccc-cccc-cccc-cccc-cccccccccccc", convA},
		{"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", convB},
		{"bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", convA},
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	for _, r := range rows {
		if _, err := tx.Exec(ctx,
			`INSERT INTO aura.paused_states (token, conversation_id, kind, question, priority, tool_call_id)
			 VALUES ($1, $2, 'approval', 'q', 5, 'tc')`, r.token, r.conv); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatalf("tx insert %s: %v", r.token, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	pending, err := s.ListPendingAll(ctx, 100)
	if err != nil {
		t.Fatalf("ListPendingAll: %v", err)
	}
	// Filter to the three tokens this test seeded (other tests may share the DB).
	seeded := map[string]bool{rows[0].token: true, rows[1].token: true, rows[2].token: true}
	var order []string
	for _, p := range pending {
		if seeded[p.Token] {
			order = append(order, p.Token)
		}
	}
	want := []string{rows[1].token, rows[2].token, rows[0].token} // aaaa < bbbb < cccc (token ASC)
	if len(order) != 3 {
		t.Fatalf("want 3 seeded pendings in the cross-thread list, got %d (%v)", len(order), order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("position %d: want token %s, got %s (cross-thread token-ASC tiebreaker broken)", i, want[i], order[i])
		}
	}
}

// TestListPendingAll_LimitDefault asserts limit<=0 falls back to 100 (mirroring
// ListRecent's <=0 guard) rather than binding a 0/negative LIMIT.
//
// The probe is seeded with a deliberately HIGH priority so it sorts within the top-100
// of the GLOBAL cross-thread read (ORDER BY priority DESC, created_at ASC, token ASC)
// even when the shared coverage-gate Postgres is flooded by other packages' parallel
// db_integration pending rows. A priority-0 probe sorted behind every other pending row
// and got buried past the 100-row default, which reddened the coverage gate (the main
// CI db_integration tier runs -p 1 against an isolated table and did not surface it).
// 100 is the constraint max (CHECK priority BETWEEN 0 AND 100, migration 0003) and the
// highest priority any seed in the suite uses is 90, so this probe is deterministically
// rank #1 (priority DESC) and always inside the default 100-row window.
func TestListPendingAll_LimitDefault(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	ctx := context.Background()
	conv := newConversation(t, pool)
	const topPriority = 100
	tok := insertPause(t, s, conv, "approval", "q", topPriority)

	for _, limit := range []int{0, -5} {
		pending, err := s.ListPendingAll(ctx, limit)
		if err != nil {
			t.Fatalf("ListPendingAll(limit=%d): %v", limit, err)
		}
		found := false
		for _, p := range pending {
			if p.Token == tok {
				found = true
			}
		}
		if !found {
			t.Errorf("ListPendingAll(limit=%d) did not default to 100 — seeded pending %s missing", limit, tok)
		}
	}
}

// TestListPendingAll_DBErrorWrapping exercises the "%w" wrap on a canceled context.
func TestListPendingAll_DBErrorWrapping(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.ListPendingAll(canceled, 100); err == nil {
		t.Error("ListPendingAll(canceled): want error, got nil")
	}
}
