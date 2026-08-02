//go:build db_integration

// Integration tests for the D-09 / CHAT-05 path-aware managed history
// (LoadManagedHistoryForBranch, context.go). Asserts the path walk feeds the SAME
// L1/L2/L2.5 ladder and that a non-branched conversation stays byte-identical to the
// linear LoadManagedHistory (the protected head / messages[0] invariant — Pitfall 3).
//
// Run via:
//
//	go test -tags db_integration -race ./internal/conversations -run TestBranch -count=1
package conversations

import (
	"fmt"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

func branchCtxCfg() ContextConfig {
	// A generous window so the ladder is a no-op (no L2.5 drop) — the test targets the
	// path-walk equivalence, not compaction.
	return ContextConfig{ContextWindow: 128000, MaxOutputTokens: 20000, ToolEvictAfterTurns: 0}
}

// TestBranchManaged_NonBranchedByteIdenticalToLinear: for a canonical-chained
// conversation, LoadManagedHistoryForBranch(default leaf) == LoadManagedHistory.
func TestBranchManaged_NonBranchedByteIdenticalToLinear(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	convID := newConversation(t, s)
	ctx := ownerCtx()

	turns := []AppendTurnParams{
		{ConversationID: convID, Seq: 1, Role: llm.RoleSystem, Content: "you are aura"},
		{ConversationID: convID, Seq: 2, Role: llm.RoleUser, Content: "hello"},
		{ConversationID: convID, Seq: 3, Role: llm.RoleAssistant, Content: "hi"},
		{ConversationID: convID, Seq: 4, Role: llm.RoleUser, Content: "more"},
		{ConversationID: convID, Seq: 5, Role: llm.RoleAssistant, Content: "ok"},
	}
	for _, tp := range turns {
		if err := s.AppendTurn(ctx, tp); err != nil {
			t.Fatalf("AppendTurn seq %d: %v", tp.Seq, err)
		}
	}
	chainCanonical(t, s, convID, 1, 2, 3, 4, 5)

	cfg := branchCtxCfg()
	linear, err := s.LoadManagedHistory(ctx, convID, cfg)
	if err != nil {
		t.Fatalf("LoadManagedHistory: %v", err)
	}
	// leafSeq <= 0 selects the canonical branch leaf.
	branch, err := s.LoadManagedHistoryForBranch(ctx, convID, 0, cfg)
	if err != nil {
		t.Fatalf("LoadManagedHistoryForBranch: %v", err)
	}
	if fmt.Sprintf("%#v", linear) != fmt.Sprintf("%#v", branch) {
		t.Errorf("managed branch (default leaf) != linear managed history:\nlinear %#v\nbranch %#v", linear, branch)
	}
	if len(branch) != 5 {
		t.Fatalf("managed branch history: want 5 messages, got %d", len(branch))
	}
}

// TestBranchManaged_ProtectedHeadAcrossBranches: through the ladder, two sibling
// branches keep a byte-identical messages[0] (system) — the CAP-04 invariant the
// cache-invariant audit also gates.
func TestBranchManaged_ProtectedHeadAcrossBranches(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	convID := newConversation(t, s)
	ctx := ownerCtx()

	head := []AppendTurnParams{
		{ConversationID: convID, Seq: 1, Role: llm.RoleSystem, Content: "you are aura"},
		{ConversationID: convID, Seq: 2, Role: llm.RoleUser, Content: "answer me"},
		{ConversationID: convID, Seq: 3, Role: llm.RoleAssistant, Content: "answer A"},
		{ConversationID: convID, Seq: 4, Role: llm.RoleAssistant, Content: "answer B"},
	}
	for _, tp := range head {
		if err := s.AppendTurn(ctx, tp); err != nil {
			t.Fatalf("AppendTurn seq %d: %v", tp.Seq, err)
		}
	}
	chainCanonical(t, s, convID, 1, 2)
	if err := s.SetBranchPointers(ctx, convID, 3, uuid.Must(uuid.NewV7()), 2); err != nil {
		t.Fatalf("set branch A: %v", err)
	}
	if err := s.SetBranchPointers(ctx, convID, 4, uuid.Must(uuid.NewV7()), 2); err != nil {
		t.Fatalf("set branch B: %v", err)
	}

	cfg := branchCtxCfg()
	histA, err := s.LoadManagedHistoryForBranch(ctx, convID, 3, cfg)
	if err != nil {
		t.Fatalf("managed branch A: %v", err)
	}
	histB, err := s.LoadManagedHistoryForBranch(ctx, convID, 4, cfg)
	if err != nil {
		t.Fatalf("managed branch B: %v", err)
	}
	if len(histA) == 0 || len(histB) == 0 {
		t.Fatal("managed branch histories must be non-empty")
	}
	if fmt.Sprintf("%#v", histA[0]) != fmt.Sprintf("%#v", histB[0]) {
		t.Errorf("messages[0] differs across branches through the ladder:\nA %#v\nB %#v", histA[0], histB[0])
	}
	last := func(h []llm.Message) string { return h[len(h)-1].Content }
	if last(histA) != "answer A" || last(histB) != "answer B" {
		t.Errorf("wrong branch leaves through ladder: A=%q B=%q", last(histA), last(histB))
	}
}

func TestManagedHistoryHardCapBoundsLinearAndBranchQueries(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	convID := newConversation(t, s)
	ctx := ownerCtx()

	turns := []AppendTurnParams{
		{ConversationID: convID, Seq: 1, Role: llm.RoleSystem, Content: "system"},
		{ConversationID: convID, Seq: 2, Role: llm.RoleUser, Content: "old user"},
		{ConversationID: convID, Seq: 3, Role: llm.RoleAssistant, Content: "old answer"},
		{ConversationID: convID, Seq: 4, Role: llm.RoleUser, Content: "middle user"},
		{ConversationID: convID, Seq: 5, Role: llm.RoleAssistant, Content: "middle answer"},
		{ConversationID: convID, Seq: 6, Role: llm.RoleUser, Content: "latest user"},
		{ConversationID: convID, Seq: 7, Role: llm.RoleAssistant, Content: "latest answer"},
	}
	for _, turn := range turns {
		if err := s.AppendTurn(ctx, turn); err != nil {
			t.Fatalf("AppendTurn seq %d: %v", turn.Seq, err)
		}
	}
	chainCanonical(t, s, convID, 1, 2, 3, 4, 5, 6, 7)

	cfg := branchCtxCfg()
	cfg.HistoryHardCapTurns = 5
	linear, err := s.LoadManagedHistory(ctx, convID, cfg)
	if err != nil {
		t.Fatalf("linear managed history: %v", err)
	}
	branch, err := s.LoadManagedHistoryForBranch(ctx, convID, 7, cfg)
	if err != nil {
		t.Fatalf("branch managed history: %v", err)
	}
	for name, history := range map[string][]llm.Message{
		"linear": linear,
		"branch": branch,
	} {
		if len(history) != 5 {
			t.Fatalf("%s history length = %d, want 5", name, len(history))
		}
		got := []string{
			history[0].Content,
			history[1].Content,
			history[2].Content,
			history[3].Content,
			history[4].Content,
		}
		want := []string{
			"system",
			"middle user",
			"middle answer",
			"latest user",
			"latest answer",
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("%s history = %v, want %v", name, got, want)
		}
	}
}
