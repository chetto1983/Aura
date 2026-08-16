//go:build db_integration

package conversations

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func countRotEvents(t *testing.T, s *Store, convID string) int {
	t.Helper()
	var n int
	if err := s.pool.QueryRow(ownerCtx(),
		"SELECT count(*) FROM aura.context_rot_events WHERE conversation_id = $1", convID,
	).Scan(&n); err != nil {
		t.Fatalf("count rot events: %v", err)
	}
	return n
}

// rotEventAction returns the action of the single context_rot_events row for a
// conversation (the L2.5 smoke contract asserts it is 'hard_drop_pairs').
func rotEventAction(t *testing.T, s *Store, convID string) string {
	t.Helper()
	var action string
	if err := s.pool.QueryRow(ownerCtx(),
		"SELECT action FROM aura.context_rot_events WHERE conversation_id = $1 LIMIT 1", convID,
	).Scan(&action); err != nil {
		t.Fatalf("read rot event action: %v", err)
	}
	return action
}

// TestLoadManagedHistory_SC1_NoRotEventOnL1Alone: a tool-output-bloated history is
// brought under budget by L1 alone → ZERO context_rot_events rows in the DB (SC-1).
func TestLoadManagedHistory_SC1_NoRotEventOnL1Alone(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	convID := newConversation(t, s)
	ctx := ownerCtx()

	bigTool := strings.Repeat("tool output token ", 4000)
	turns := []AppendTurnParams{
		{ConversationID: convID, Seq: 1, Role: llm.RoleSystem, Content: "you are aura"},
		{ConversationID: convID, Seq: 2, Role: llm.RoleUser, Content: "go"},
		{ConversationID: convID, Seq: 3, Role: llm.RoleAssistant, Content: "ok"},
		{ConversationID: convID, Seq: 4, Role: llm.RoleTool, Content: bigTool, ToolCallID: "call_a"},
		{ConversationID: convID, Seq: 20, Role: llm.RoleUser, Content: "more"},
	}
	for _, tp := range turns {
		if err := s.AppendTurn(ctx, tp); err != nil {
			t.Fatalf("AppendTurn seq %d: %v", tp.Seq, err)
		}
	}

	cfg := ContextConfig{ContextWindow: 50000, MaxOutputTokens: 1000, ToolEvictAfterTurns: 10}
	msgs, err := s.LoadManagedHistory(ctx, convID, cfg)
	if err != nil {
		t.Fatalf("LoadManagedHistory: %v", err)
	}
	if n := countRotEvents(t, s, convID); n != 0 {
		t.Errorf("SC-1 violated: L1 alone must write zero context_rot_events, got %d", n)
	}
	if len(msgs) != 5 {
		t.Errorf("L1 must not drop turns, got %d", len(msgs))
	}
	if !strings.Contains(msgs[3].Content, "read_tool_output") {
		t.Errorf("old tool turn must be L1-evicted, got %q", msgs[3].Content)
	}
}

// TestLoadManagedHistory_L2_5_WritesRotEvent: a user/assistant-text-bloated history
// past the hard cap drops the oldest pair and writes exactly one DB rot-event row.
func TestLoadManagedHistory_L2_5_WritesRotEvent(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	convID := newConversation(t, s)
	ctx := ownerCtx()

	big := strings.Repeat("word ", 3000)
	turns := []AppendTurnParams{
		{ConversationID: convID, Seq: 1, Role: llm.RoleSystem, Content: "sys"},
		{ConversationID: convID, Seq: 2, Role: llm.RoleUser, Content: big},
		{ConversationID: convID, Seq: 3, Role: llm.RoleAssistant, Content: big},
		{ConversationID: convID, Seq: 4, Role: llm.RoleUser, Content: "recent q"},
		{ConversationID: convID, Seq: 5, Role: llm.RoleAssistant, Content: "recent a"},
	}
	for _, tp := range turns {
		if err := s.AppendTurn(ctx, tp); err != nil {
			t.Fatalf("AppendTurn seq %d: %v", tp.Seq, err)
		}
	}

	cfg := ContextConfig{ContextWindow: 8000, MaxOutputTokens: 1, ToolEvictAfterTurns: 100}
	msgs, err := s.LoadManagedHistory(ctx, convID, cfg)
	if err != nil {
		t.Fatalf("LoadManagedHistory: %v", err)
	}
	if n := countRotEvents(t, s, convID); n != 1 {
		t.Errorf("L2.5 must write exactly one rot event, got %d", n)
	}
	if action := rotEventAction(t, s, convID); action != rotActionHardDropPairs {
		t.Errorf("L2.5 rot event action = %q, want %q", action, rotActionHardDropPairs)
	}
	if (len(msgs)-1)%2 != 0 {
		t.Errorf("non-system remainder must be even after L2.5, got %d", len(msgs)-1)
	}
}
