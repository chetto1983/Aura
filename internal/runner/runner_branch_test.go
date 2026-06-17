package runner

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
)

// TestTurnBranch_LoadsSelectedBranchHistory proves the D-09 re-run-from-a-point drives a
// fresh agent round over the SELECTED branch path (LoadManagedHistoryForBranch with the
// given leaf) rather than the linear full history — and with NO fresh user message
// (continue-after-resume). The fake records the leaf it was asked to load.
func TestTurnBranch_LoadsSelectedBranchHistory(t *testing.T) {
	client := agenttest.NewFakeClient(agenttest.ToolCallTurn(textResponseCall("c1", "re-run done")))
	r, conv, _ := newTestRunner(t, client)
	convID := newConvID(t)
	mustCreate(t, r, convID)

	const selectedLeaf = 7
	if _, err := drain(r.TurnBranch(context.Background(), convID, selectedLeaf)); err != nil {
		t.Fatalf("TurnBranch must not error: %v", err)
	}

	conv.mu.Lock()
	gotLeaf := conv.lastBranchLeaf
	conv.mu.Unlock()
	if gotLeaf != selectedLeaf {
		t.Fatalf("TurnBranch loaded branch leaf %d, want %d (LoadManagedHistoryForBranch(selected))", gotLeaf, selectedLeaf)
	}
}

// TestTurn_LinearDoesNotTouchBranchLoader proves the default Turn path stays linear
// (LoadManagedHistory) and never calls the branch loader — the non-branched regression
// guard (branchLeaf 0 dispatch).
func TestTurn_LinearDoesNotTouchBranchLoader(t *testing.T) {
	client := agenttest.NewFakeClient(agenttest.ToolCallTurn(textResponseCall("c1", "done")))
	r, conv, _ := newTestRunner(t, client)
	convID := newConvID(t)
	mustCreate(t, r, convID)

	if _, err := drain(r.Turn(context.Background(), convID, userPtr("hi"))); err != nil {
		t.Fatalf("linear Turn must not error: %v", err)
	}
	conv.mu.Lock()
	gotLeaf := conv.lastBranchLeaf
	conv.mu.Unlock()
	if gotLeaf != 0 {
		t.Fatalf("linear Turn touched the branch loader (leaf=%d); want 0 (LoadManagedHistory only)", gotLeaf)
	}
}
