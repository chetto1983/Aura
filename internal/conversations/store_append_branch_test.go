//go:build db_integration

// Integration tests for the path every append records (spec 2026-10-27, D4): parent_seq is
// the turn the new one continues, and branch_id is its parent's, or a fresh one when the
// parent already has a child. Run via:
//
//	go test -tags db_integration -race ./internal/conversations -run TestAppendBranch -count=1
package conversations

import (
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

type turnPointers struct {
	branch uuid.UUID
	parent int
}

func pointersOf(t *testing.T, s *Store, convID string, seq int) turnPointers {
	t.Helper()
	id, err := db.ParseUUID("conversation_id", convID)
	if err != nil {
		t.Fatal(err)
	}
	var got turnPointers
	if err := s.scoped(ownerCtx(), func(q *sqlc.Queries) error {
		row, qErr := q.GetTurnPointers(ownerCtx(), sqlc.GetTurnPointersParams{ConversationID: id, Seq: int32(seq)})
		if qErr != nil {
			return qErr
		}
		got.branch = row.BranchID.Bytes
		if row.ParentSeq.Valid {
			got.parent = int(row.ParentSeq.Int32)
		}
		return nil
	}); err != nil {
		t.Fatalf("pointers of seq %d: %v", seq, err)
	}
	return got
}

func appendAt(t *testing.T, s *Store, convID, role, content string, parent *int) int {
	t.Helper()
	seq, err := s.AppendTurnSeq(ownerCtx(), AppendTurnParams{
		ConversationID: convID, Role: role, Content: content, ParentSeq: parent,
	})
	if err != nil {
		t.Fatalf("append %q: %v", content, err)
	}
	return seq
}

// seedThree writes U1, A1, U2 the way every run appends: no explicit parent.
func seedThree(t *testing.T, s *Store) string {
	t.Helper()
	convID := newConversation(t, s)
	for _, turn := range []struct{ role, content string }{
		{llm.RoleUser, "U1"}, {llm.RoleAssistant, "A1"}, {llm.RoleUser, "U2"},
	} {
		appendAt(t, s, convID, turn.role, turn.content, nil)
	}
	return convID
}

func TestAppendBranch_AChainKeepsOneBranch(t *testing.T) {
	s := newStore(t, migratedPool(t))
	convID := seedThree(t, s)
	for seq, wantParent := range map[int]int{1: 0, 2: 1, 3: 2} {
		got := pointersOf(t, s, convID, seq)
		if got.parent != wantParent || got.branch != CanonicalBranchID {
			t.Errorf("seq %d = %+v, want parent %d on the canonical branch", seq, got, wantParent)
		}
	}
}

func TestAppendBranch_ASecondChildStartsABranchItsDescendantsKeep(t *testing.T) {
	s := newStore(t, migratedPool(t))
	convID := seedThree(t, s)

	sibling := appendAt(t, s, convID, llm.RoleUser, "U2 on another branch", new(2))
	got := pointersOf(t, s, convID, sibling)
	if got.parent != 2 || got.branch == CanonicalBranchID {
		t.Fatalf("a second child of seq 2 = %+v, want parent 2 on a fresh branch", got)
	}
	child := appendAt(t, s, convID, llm.RoleAssistant, "its answer", nil)
	if next := pointersOf(t, s, convID, child); next.parent != sibling || next.branch != got.branch {
		t.Fatalf("the turn after it = %+v, want parent %d on branch %s", next, sibling, got.branch)
	}
	if third := appendAt(t, s, convID, llm.RoleUser, "a third child", new(2)); pointersOf(t, s, convID, third).branch == got.branch {
		t.Fatal("a third child of seq 2 reused the second's branch")
	}
}

func TestAppendBranch_ContinuingALeafInheritsItsBranch(t *testing.T) {
	s := newStore(t, migratedPool(t))
	convID := seedThree(t, s)
	appendAt(t, s, convID, llm.RoleUser, "U2 elsewhere", new(2))

	resumed := appendAt(t, s, convID, llm.RoleAssistant, "A2 under U2", new(3))
	if got := pointersOf(t, s, convID, resumed); got.parent != 3 || got.branch != CanonicalBranchID {
		t.Fatalf("continuing leaf 3 = %+v, want parent 3 on the canonical branch", got)
	}
}

func TestAppendBranch_ARootSiblingHasNoParent(t *testing.T) {
	s := newStore(t, migratedPool(t))
	convID := seedThree(t, s)

	root := appendAt(t, s, convID, llm.RoleUser, "U1 rewritten", new(0))
	if got := pointersOf(t, s, convID, root); got.parent != 0 || got.branch == CanonicalBranchID {
		t.Fatalf("a second root = %+v, want no parent on a fresh branch", got)
	}
}

func TestAppendBranch_AnUnknownParentWritesNothing(t *testing.T) {
	s := newStore(t, migratedPool(t))
	convID := seedThree(t, s)

	_, err := s.AppendTurnSeq(ownerCtx(), AppendTurnParams{
		ConversationID: convID, Role: llm.RoleUser, Content: "orphan", ParentSeq: new(99),
	})
	if !errors.Is(err, ErrTurnNotFound) {
		t.Fatalf("parent 99 = %v, want ErrTurnNotFound", err)
	}
	if n, err := s.CountTurns(ownerCtx(), convID); err != nil || n != 3 {
		t.Fatalf("turns after the refusal = %d (%v), want 3", n, err)
	}
}

func TestAppendBranch_AForkedTurnsDescendantsStayOnItsBranch(t *testing.T) {
	s := newStore(t, migratedPool(t))
	convID := seedThree(t, s)

	forked, err := s.ForkBranch(ownerCtx(), convID, 3, llm.RoleUser, "U2 edited")
	if err != nil {
		t.Fatalf("ForkBranch: %v", err)
	}
	fork := pointersOf(t, s, convID, forked)
	if fork.parent != 2 || fork.branch == CanonicalBranchID {
		t.Fatalf("the fork = %+v, want a sibling of seq 3 on a fresh branch", fork)
	}
	answer := appendAt(t, s, convID, llm.RoleAssistant, "A2 to the edit", nil)
	if got := pointersOf(t, s, convID, answer); got.parent != forked || got.branch != fork.branch {
		t.Fatalf("the answer to the fork = %+v, want parent %d on branch %s", got, forked, fork.branch)
	}
}
