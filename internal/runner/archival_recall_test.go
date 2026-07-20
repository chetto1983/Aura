package runner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
)

// TestArchivalRecall_ComposedAndScopedByOwnerID proves the L4 recall seam (amendment
// #21) injects its block into messages[1] AND is called with owner.ID — the same value
// the memory-MCP write path scopes on. owner.Name would silently query the wrong scope,
// so this pins the correctness point.
func TestArchivalRecall_ComposedAndScopedByOwnerID(t *testing.T) {
	r, conv, _ := newTestRunner(t, agenttest.NewFakeClient(agenttest.ToolCallTurn(textResponseCall("c1", "ok"))))
	convID := newConvID(t)
	mustCreate(t, r, convID)

	var seenID, seenQuery string
	r.archivalRecaller = func(_ context.Context, userIdentifier, query string) (string, error) {
		seenID, seenQuery = userIdentifier, query
		return "## Relevant long-term memory\nUser prefers Italian food.", nil
	}
	r.alwaysBlock = func() string { return "<skills always>body</skills always>" }

	if _, err := drain(r.Turn(context.Background(), convID, userPtr("what should I eat?"))); err != nil {
		t.Fatalf("turn with recall: %v", err)
	}
	if seenID != "00000000-0000-0000-0000-000000000001" {
		t.Fatalf("recall scoped by %q; want owner.ID (the memory write-path scope), not owner.Name", seenID)
	}
	if seenQuery != "what should I eat?" {
		t.Fatalf("recall query = %q; want the current user message threaded through for top-K relevance", seenQuery)
	}
	block := conv.lastCfg.AlwaysBlock
	recallAt := strings.Index(block, "prefers Italian food")
	skillsAt := strings.Index(block, "<skills always>")
	if recallAt < 0 {
		t.Fatalf("recall block not injected into AlwaysBlock:\n%s", block)
	}
	if skillsAt >= 0 && recallAt > skillsAt {
		t.Fatalf("recall should precede the skills block, got:\n%s", block)
	}
}

// TestArchivalRecall_EmptyOmitted proves an empty recall (nothing stored) contributes
// no block — the AlwaysBlock degrades to the skills block alone.
func TestArchivalRecall_EmptyOmitted(t *testing.T) {
	r, conv, _ := newTestRunner(t, agenttest.NewFakeClient(agenttest.ToolCallTurn(textResponseCall("c1", "ok"))))
	convID := newConvID(t)
	mustCreate(t, r, convID)

	r.archivalRecaller = func(context.Context, string, string) (string, error) { return "", nil }
	r.alwaysBlock = func() string { return "SKILLS-ONLY" }

	if _, err := drain(r.Turn(context.Background(), convID, userPtr("hi"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	if conv.lastCfg.AlwaysBlock != "SKILLS-ONLY" {
		t.Fatalf("AlwaysBlock = %q, want skills-only when recall is empty", conv.lastCfg.AlwaysBlock)
	}
}

// TestArchivalRecall_ErrorSurfaces proves the seam propagates a recall error (the
// composition-root provider fail-softs, but the runner contract must surface a real
// error so a broken wiring is never silently swallowed at this layer).
func TestArchivalRecall_ErrorSurfaces(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient(agenttest.ToolCallTurn(textResponseCall("c1", "ok"))))
	convID := newConvID(t)
	mustCreate(t, r, convID)

	r.archivalRecaller = func(context.Context, string, string) (string, error) {
		return "", errors.New("memory sidecar down")
	}

	if _, err := drain(r.Turn(context.Background(), convID, userPtr("hi"))); err == nil {
		t.Fatal("expected the recall error to surface on the turn")
	}
}
