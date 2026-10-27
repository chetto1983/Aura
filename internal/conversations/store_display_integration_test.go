//go:build db_integration

// Integration tests for the reads a reader of the conversation gets (spec 2026-10-27, D5 and
// D12): the snapshot's tree and the share's single path. Run via:
//
//	go test -tags db_integration -race ./internal/conversations -run 'TestDisplayTurns|TestLatestBranch' -count=1
package conversations

import (
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestDisplayTurns_EveryBranchWithTheTurnItContinues(t *testing.T) {
	s := newStore(t, migratedPool(t))
	convID := seedThree(t, s)
	forked, err := s.ForkBranch(ownerCtx(), convID, 3, llm.RoleUser, "U2 edited")
	if err != nil {
		t.Fatalf("ForkBranch: %v", err)
	}
	answer := appendAt(t, s, convID, llm.RoleAssistant, "A2 to the edit", nil)

	turns, err := s.LoadDisplayTurns(ownerCtx(), convID)
	if err != nil {
		t.Fatalf("LoadDisplayTurns: %v", err)
	}
	want := []struct {
		seq, parent int
		content     string
	}{{1, 0, "U1"}, {2, 1, "A1"}, {3, 2, "U2"}, {forked, 2, "U2 edited"}, {answer, forked, "A2 to the edit"}}
	if len(turns) != len(want) {
		t.Fatalf("display turns = %+v, want %d", turns, len(want))
	}
	for i, w := range want {
		got := turns[i]
		if got.Seq != w.seq || got.ParentSeq != w.parent || got.Message.Content != w.content {
			t.Errorf("turn %d = seq %d parent %d %q, want seq %d parent %d %q",
				i, got.Seq, got.ParentSeq, got.Message.Content, w.seq, w.parent, w.content)
		}
	}
}

// TestDisplayTurns_AnOrphanResultsChildContinuesItsParent: a tool result with no call is not
// drawn, as the model never sees it, and the turn written after it hangs from the turn before.
func TestDisplayTurns_AnOrphanResultsChildContinuesItsParent(t *testing.T) {
	s := newStore(t, migratedPool(t))
	convID := newConversation(t, s)
	appendAt(t, s, convID, llm.RoleUser, "U1", nil)
	if _, err := s.AppendTurnSeq(ownerCtx(), AppendTurnParams{
		ConversationID: convID, Role: llm.RoleTool, ToolCallID: "stray", Content: "orphan",
	}); err != nil {
		t.Fatalf("append orphan: %v", err)
	}
	reply := appendAt(t, s, convID, llm.RoleAssistant, "A1", nil)

	turns, err := s.LoadDisplayTurns(ownerCtx(), convID)
	if err != nil {
		t.Fatalf("LoadDisplayTurns: %v", err)
	}
	if len(turns) != 2 || turns[1].Seq != reply || turns[1].ParentSeq != 1 {
		t.Fatalf("display turns = %+v, want U1 then A1 hanging from seq 1", turns)
	}
}

func TestLatestBranch_ANonPositiveLeafIsTheBranchWrittenLast(t *testing.T) {
	s := newStore(t, migratedPool(t))
	convID := seedThree(t, s)
	if _, err := s.ForkBranch(ownerCtx(), convID, 1, llm.RoleUser, "U1 rewritten"); err != nil {
		t.Fatalf("ForkBranch: %v", err)
	}

	hist, err := s.LoadBranchHistory(ownerCtx(), convID, 0)
	if err != nil {
		t.Fatalf("LoadBranchHistory: %v", err)
	}
	if len(hist) != 1 || hist[0].Content != "U1 rewritten" {
		t.Fatalf("latest branch = %+v, want only the rewritten root", hist)
	}
}
