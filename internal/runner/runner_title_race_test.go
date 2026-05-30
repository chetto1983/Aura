package runner

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
)

// TestAutoTitle_DefensiveHistoryCopy_WR03 is the regression for WR-03: the auto-title
// worker must own a defensive snapshot of history, so a caller mutating the history
// slice in-place after maybeAutoTitle returns cannot race the worker. Run under
// -race: pre-fix (closing over the caller's slice) the in-place write below races the
// worker's read; post-fix the worker reads its own copy and the title still resolves.
func TestAutoTitle_DefensiveHistoryCopy_WR03(t *testing.T) {
	client := agenttest.NewFakeClient(
		agenttest.TextChunks("stop", "A Title From The Snapshot"),
	)
	r, conv, _ := newTestRunner(t, client)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)
	// Reach the seq>=3 auto-title threshold.
	seedSystemTurn(t, r, convID)
	if err := r.Conv.AppendTurn(ctx, conversations.AppendTurnParams{ConversationID: convID, Seq: 2, Role: llm.RoleUser, Content: "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Conv.AppendTurn(ctx, conversations.AppendTurnParams{ConversationID: convID, Seq: 3, Role: llm.RoleAssistant, Content: "hi there"}); err != nil {
		t.Fatal(err)
	}

	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "system"},
		{Role: llm.RoleUser, Content: "hello"},
		{Role: llm.RoleAssistant, Content: "hi there"},
	}
	r.maybeAutoTitle(ctx, convID, history)
	// Mutate the caller's slice in-place WHILE the worker may be running. With the
	// defensive copy this is safe; without it, -race flags the read/write overlap.
	for i := range history {
		history[i].Content = "mutated"
	}

	if err := r.Stop(ctx, convID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	c, err := conv.Get(ctx, convID)
	if err != nil {
		t.Fatal(err)
	}
	if !c.TitleSet || c.Title == "" {
		t.Fatalf("WR-03: title should resolve from the worker's snapshot, got TitleSet=%v Title=%q", c.TitleSet, c.Title)
	}
}
