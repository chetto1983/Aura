package runner

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/llm"
)

// TestTurnBranch_LoadsSelectedBranchHistory proves the D-09 re-run-from-a-point drives a
// fresh agent round over the path that ends at the given leaf, with NO fresh user message
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
		t.Fatalf("TurnBranch loaded leaf %d, want %d", gotLeaf, selectedLeaf)
	}
}

// TestTurn_LoadsThePathOfTheTurnItAppends: a run's history is the path that ends at the
// user turn it appended. It used to be every turn in seq order, which after an edit sent
// the model the old branch and the new one interleaved (measured 2026-10-27).
func TestTurn_LoadsThePathOfTheTurnItAppends(t *testing.T) {
	client := agenttest.NewFakeClient(agenttest.ToolCallTurn(textResponseCall("c1", "done")))
	r, conv, _ := newTestRunner(t, client)
	convID := newConvID(t)
	mustCreate(t, r, convID)

	if _, err := drain(r.Turn(context.Background(), convID, new("hi"))); err != nil {
		t.Fatalf("Turn: %v", err)
	}
	conv.mu.Lock()
	gotLeaf := conv.lastBranchLeaf
	user := conv.turns[convID][0]
	conv.mu.Unlock()
	if user.Role != llm.RoleUser || gotLeaf != user.Seq {
		t.Fatalf("Turn loaded leaf %d, want the user turn it appended (%s seq %d)", gotLeaf, user.Role, user.Seq)
	}
}

// TestTurn_ContinuesTheBranchTheClientNames: WithBranchLeaf makes the appended user turn a
// child of that leaf; without it the turn continues the one written last.
func TestTurn_ContinuesTheBranchTheClientNames(t *testing.T) {
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(textResponseCall("c1", "first")),
		agenttest.ToolCallTurn(textResponseCall("c2", "second")),
	)
	r, conv, _ := newTestRunner(t, client)
	convID := newConvID(t)
	mustCreate(t, r, convID)

	if _, err := drain(r.Turn(context.Background(), convID, new("plain"))); err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if _, err := drain(r.Turn(WithBranchLeaf(context.Background(), 1), convID, new("under one"))); err != nil {
		t.Fatalf("Turn on a named branch: %v", err)
	}
	conv.mu.Lock()
	defer conv.mu.Unlock()
	var plain, named *int
	for _, turn := range conv.turns[convID] {
		switch turn.Content {
		case "plain":
			plain = turn.ParentSeq
		case "under one":
			named = turn.ParentSeq
		}
	}
	if plain != nil {
		t.Errorf("a turn with no branch named got parent %d, want the default (nil)", *plain)
	}
	if named == nil || *named != 1 {
		t.Errorf("a turn under leaf 1 got parent %v, want 1", named)
	}
}

// TestWithBranchLeaf_IgnoresANonPositiveSeq: 0 or less names no branch.
func TestWithBranchLeaf_IgnoresANonPositiveSeq(t *testing.T) {
	for _, seq := range []int{0, -3} {
		if got := branchLeaf(WithBranchLeaf(context.Background(), seq)); got != 0 {
			t.Errorf("WithBranchLeaf(%d) = %d, want nothing stored", seq, got)
		}
	}
}

// TestTurnHead_RecordsWhereEachRunStarted: the user turn a run appended, the fork it re-ran,
// and nothing for a resume. A second record does not overwrite the first: a leftover steer
// appends a second user turn after the client's.
func TestTurnHead_RecordsWhereEachRunStarted(t *testing.T) {
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(textResponseCall("c1", "one")),
		agenttest.ToolCallTurn(textResponseCall("c2", "two")),
		agenttest.ToolCallTurn(textResponseCall("c3", "three")),
	)
	r, conv, _ := newTestRunner(t, client)
	convID := newConvID(t)
	mustCreate(t, r, convID)

	var sent TurnHead
	if _, err := drain(r.Turn(WithTurnHead(context.Background(), &sent), convID, new("hello"))); err != nil {
		t.Fatalf("Turn: %v", err)
	}
	conv.mu.Lock()
	userSeq := conv.turns[convID][0].Seq
	conv.mu.Unlock()
	if sent.Seq() != userSeq {
		t.Errorf("a send recorded head %d, want the user turn %d", sent.Seq(), userSeq)
	}

	var forked TurnHead
	if _, err := drain(r.TurnBranch(WithTurnHead(context.Background(), &forked), convID, 9)); err != nil {
		t.Fatalf("TurnBranch: %v", err)
	}
	if forked.Seq() != 9 {
		t.Errorf("a re-run recorded head %d, want the fork 9", forked.Seq())
	}

	var resumed TurnHead
	if _, err := drain(r.Turn(WithTurnHead(context.Background(), &resumed), convID, nil)); err != nil {
		t.Fatalf("resume Turn: %v", err)
	}
	if resumed.Seq() != 0 {
		t.Errorf("a resume recorded head %d, want none", resumed.Seq())
	}

	ctx := WithTurnHead(context.Background(), &sent)
	recordTurnHead(ctx, 42)
	if sent.Seq() != userSeq {
		t.Errorf("a second record moved the head to %d, want it kept at %d", sent.Seq(), userSeq)
	}
}
