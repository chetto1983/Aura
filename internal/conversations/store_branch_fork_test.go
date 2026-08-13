//go:build db_integration

// Integration tests for the D-09 / CHAT-05 branch FORK + branch LIST seams
// (store_branch.go ForkBranch / ListBranches) — the edit-a-user-turn / regenerate write
// path plan 25-07 wires onto the agui re-run. Requires the live Postgres stack with
// migrations applied through 0017 (see store_branch_test.go header).
//
// Run via:
//
//	go test -tags db_integration -race ./internal/conversations -run TestBranchFork -count=1
//
// No-skip-as-green: envOrSkip (store_test.go) t.Fatals under $CI when the DSN is unset.
package conversations

import (
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// seedLinear appends a canonical linear conversation (system + user + assistant) and
// chains the canonical parent pointers, returning the conv id. This is the pre-edit
// state a fork diverges from.
func seedLinear(t *testing.T, s *Store) string {
	t.Helper()
	convID := newConversation(t, s)
	ctx := ownerCtx()
	for _, tp := range []AppendTurnParams{
		{ConversationID: convID, Seq: 1, Role: llm.RoleSystem, Content: "you are aura"},
		{ConversationID: convID, Seq: 2, Role: llm.RoleUser, Content: "first question"},
		{ConversationID: convID, Seq: 3, Role: llm.RoleAssistant, Content: "first answer"},
	} {
		if err := s.AppendTurn(ctx, tp); err != nil {
			t.Fatalf("AppendTurn seq %d: %v", tp.Seq, err)
		}
	}
	chainCanonical(t, s, convID, 1, 2, 3)
	return convID
}

// TestBranchFork_EditUserTurnCreatesSibling proves editing a user turn forks a NEW sibling
// branch (chained off the diverging turn's PARENT) while the OLD branch stays queryable —
// the full-tree contract (RESEARCH OQ3). The new branch's path replaces the edited user
// turn; the original path still reconstructs unchanged.
// TestBranchFork_ProductionAppendChainsParentSeq is the regression for the parent_seq
// data-loss bug. Every other test in this file seeds through chainCanonical, which sets
// the pointers BY HAND — so the suite validated a state the production append path did
// not produce, and stayed green while every appended turn carried parent_seq = NULL.
//
// It seeds with AppendTurn ONLY, and it forks DEEP. Forking at seq 2 cannot tell the
// two worlds apart: chained, the path is [system, edited]; broken, the walk dies at the
// NULL and the CTE's separate head re-adds the system turn — length 2 either way. That
// is exactly why the bug survived. Forking at seq 4 makes the correct path [1,2,3,leaf]
// and the broken one [system, leaf]: 4 against 2.
func TestBranchFork_ProductionAppendChainsParentSeq(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	ctx := ownerCtx()
	convID := newConversation(t, s)

	for _, tp := range []AppendTurnParams{
		{ConversationID: convID, Seq: 1, Role: llm.RoleSystem, Content: "you are aura"},
		{ConversationID: convID, Seq: 2, Role: llm.RoleUser, Content: "first question"},
		{ConversationID: convID, Seq: 3, Role: llm.RoleAssistant, Content: "first answer"},
		{ConversationID: convID, Seq: 4, Role: llm.RoleUser, Content: "second question"},
		{ConversationID: convID, Seq: 5, Role: llm.RoleAssistant, Content: "second answer"},
	} {
		if err := s.AppendTurn(ctx, tp); err != nil {
			t.Fatalf("AppendTurn seq %d: %v", tp.Seq, err)
		}
	}
	// Deliberately NO chainCanonical: the point is that the production path chains itself.

	newSeq, _, err := s.ForkBranch(ctx, convID, 4, llm.RoleUser, "edited second question")
	if err != nil {
		t.Fatalf("ForkBranch(seq 4): %v", err)
	}
	path, err := s.LoadBranchHistory(ctx, convID, newSeq)
	if err != nil {
		t.Fatalf("LoadBranchHistory(new): %v", err)
	}
	if len(path) != 4 {
		var got []string
		for _, turn := range path {
			got = append(got, turn.Content)
		}
		t.Fatalf("continued branch reconstructed %d turns %q, want 4 "+
			"([system, first question, first answer, edited second question]) — a short "+
			"path means the parent_seq walk died on a NULL and the model lost the prior "+
			"conversation", len(path), got)
	}
	for i, want := range []string{"you are aura", "first question", "first answer", "edited second question"} {
		if path[i].Content != want {
			t.Errorf("path[%d] = %q, want %q", i, path[i].Content, want)
		}
	}
}

func TestBranchFork_EditUserTurnCreatesSibling(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	ctx := ownerCtx()
	convID := seedLinear(t, s)

	// The original canonical leaf (assistant turn seq 3) still walks the full linear path.
	origLeaf, err := s.CanonicalBranchLeaf(ctx, convID)
	if err != nil {
		t.Fatalf("CanonicalBranchLeaf: %v", err)
	}
	origPath, err := s.LoadBranchHistory(ctx, convID, origLeaf)
	if err != nil {
		t.Fatalf("LoadBranchHistory(orig): %v", err)
	}
	if len(origPath) != 3 {
		t.Fatalf("orig path len = %d, want 3 (system+user+assistant)", len(origPath))
	}

	// Edit the user turn (seq 2): fork a sibling chained off seq 2's parent (seq 1).
	newSeq, branchID, err := s.ForkBranch(ctx, convID, 2, llm.RoleUser, "edited question")
	if err != nil {
		t.Fatalf("ForkBranch(edit user turn): %v", err)
	}
	if newSeq <= 3 {
		t.Fatalf("forked seq = %d, want a fresh seq > 3", newSeq)
	}
	if branchID == CanonicalBranchID {
		t.Fatalf("forked branch id is the canonical sentinel; want a fresh uuid")
	}

	// The NEW branch path: system (seq 1) + the edited user turn — the old user/assistant
	// turns are NOT on this path (they belong to the original branch).
	newPath, err := s.LoadBranchHistory(ctx, convID, newSeq)
	if err != nil {
		t.Fatalf("LoadBranchHistory(new): %v", err)
	}
	if len(newPath) != 2 {
		t.Fatalf("new path len = %d, want 2 (system + edited user)", len(newPath))
	}
	if newPath[0].Content != "you are aura" {
		t.Errorf("new path head = %q, want the byte-identical system turn", newPath[0].Content)
	}
	if newPath[1].Content != "edited question" {
		t.Errorf("new path tail = %q, want the edited user content", newPath[1].Content)
	}

	// The OLD branch is still fully queryable (full-tree: the original path is unchanged).
	stillOrig, err := s.LoadBranchHistory(ctx, convID, origLeaf)
	if err != nil {
		t.Fatalf("LoadBranchHistory(orig after fork): %v", err)
	}
	if len(stillOrig) != 3 || stillOrig[1].Content != "first question" {
		t.Fatalf("old branch changed after fork: %+v", stillOrig)
	}

	// The protected head (system turn) is byte-identical across the two branches — the
	// CAP-04 cache invariant (only body turns differ per branch, Pitfall 3).
	if newPath[0].Content != origPath[0].Content || newPath[0].Role != origPath[0].Role {
		t.Errorf("head differs across branches: new=%+v orig=%+v", newPath[0], origPath[0])
	}
}

// TestBranchFork_ListsSiblingBranches proves ListBranches reports BOTH the original leaf
// and the forked sibling (the BranchPicker's navigable set), and a non-branched
// conversation reports exactly one branch (the picker stays hidden).
func TestBranchFork_ListsSiblingBranches(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	ctx := ownerCtx()
	convID := seedLinear(t, s)

	branches, err := s.ListBranches(ctx, convID)
	if err != nil {
		t.Fatalf("ListBranches(pre-fork): %v", err)
	}
	if len(branches) != 1 {
		t.Fatalf("pre-fork branches = %d, want 1 (the canonical leaf — picker hidden)", len(branches))
	}
	if branches[0].LeafSeq != 3 {
		t.Errorf("canonical leaf seq = %d, want 3", branches[0].LeafSeq)
	}

	newSeq, _, err := s.ForkBranch(ctx, convID, 2, llm.RoleUser, "edited question")
	if err != nil {
		t.Fatalf("ForkBranch: %v", err)
	}

	branches, err = s.ListBranches(ctx, convID)
	if err != nil {
		t.Fatalf("ListBranches(post-fork): %v", err)
	}
	if len(branches) != 2 {
		t.Fatalf("post-fork branches = %d, want 2 (original leaf + forked sibling)", len(branches))
	}
	var sawOrig, sawNew bool
	for _, b := range branches {
		switch b.LeafSeq {
		case 3:
			sawOrig = true
		case newSeq:
			sawNew = true
		}
	}
	if !sawOrig || !sawNew {
		t.Fatalf("branch leaves missing one of {3,%d}: %+v", newSeq, branches)
	}
}

// TestBranchFork_UnknownDivergeSeq proves a fork targeting a non-existent seq returns
// ErrTurnNotFound (mapped to 404 at the REST boundary, T-25-26) and writes nothing.
func TestBranchFork_UnknownDivergeSeq(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	ctx := ownerCtx()
	convID := seedLinear(t, s)

	_, _, err := s.ForkBranch(ctx, convID, 999, llm.RoleUser, "ghost")
	if !errors.Is(err, ErrTurnNotFound) {
		t.Fatalf("ForkBranch(unknown seq) err = %v, want ErrTurnNotFound", err)
	}
	if got := countTurns(t, pool, convID); got != 3 {
		t.Errorf("turn count after failed fork = %d, want 3 (no partial write)", got)
	}
}
