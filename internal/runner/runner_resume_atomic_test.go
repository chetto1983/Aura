package runner

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/askuser"
)

// TestSubmitAnswer_DuplicateResumeInjectsExactlyOneAnswer pins M-02: SubmitAnswer
// must claim the pause (MarkResumed's RowsAffected==0 gate) BEFORE injecting the
// answer turn. The old order injected first, so a duplicate resume appended a SECOND
// answer turn for the same tool_call before the gate rejected it — bricking the
// rehydrated round (two tool results for one tool_call). A retry must now return
// ErrPauseNotFound and leave the turn count unchanged.
func TestSubmitAnswer_DuplicateResumeInjectsExactlyOneAnswer(t *testing.T) {
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(askUserCall("call-1", "What city?", "clarification")),
	)
	r, conv, pause := newTestRunner(t, client)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)

	if _, err := drain(r.Turn(ctx, convID, userPtr("Where am I?"))); err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	pending, err := pause.ListPending(ctx, convID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("want 1 pending, got %d (err=%v)", len(pending), err)
	}
	token := pending[0].Token

	// First resume claims the pause and injects exactly one answer turn.
	if _, err := r.SubmitAnswer(ctx, token, ResponseInput{Action: askuser.ActionAccept, Content: "Rome"}); err != nil {
		t.Fatalf("first submit: %v", err)
	}
	countAfterFirst, err := conv.CountTurns(ctx, convID)
	if err != nil {
		t.Fatalf("count turns: %v", err)
	}

	// Duplicate resume (same token): the gate must reject it BEFORE any inject.
	_, dupErr := r.SubmitAnswer(ctx, token, ResponseInput{Action: askuser.ActionAccept, Content: "Rome again"})
	if !errors.Is(dupErr, askuser.ErrPauseNotFound) {
		t.Fatalf("a duplicate resume must return ErrPauseNotFound, got %v", dupErr)
	}
	countAfterDup, err := conv.CountTurns(ctx, convID)
	if err != nil {
		t.Fatalf("count turns: %v", err)
	}
	if countAfterDup != countAfterFirst {
		t.Fatalf("a duplicate resume must NOT append a second answer turn: turns %d -> %d", countAfterFirst, countAfterDup)
	}
}
