package runner

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/askuser"
)

// TestSubmitAnswers_DuplicateBatchInjectsExactlyOneAnswerPerPause is the LOOP-02/F-004
// unit regression for the claim-all-then-append-all fix: a re-submitted batch (same
// tokens) must resolve each pause exactly once — the second batch's claim fails
// (ErrPauseNotFound) and appends NO second set of answer turns, so the rehydrated history
// stays wire-valid (no two tool results for one tool_call). The atomic all-or-nothing
// rollback is proven against live Postgres in the _integration variant; here the default
// split committer proves the ordering + idempotency contract with the in-memory fakes.
func TestResumeBatch_DuplicateInjectsExactlyOneAnswerPerPause(t *testing.T) {
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(
			askUserCall("call-1", "A?", "clarification"),
			askUserCall("call-2", "B?", "clarification"),
		),
	)
	r, conv, pause := newTestRunner(t, client)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)

	if _, err := drain(r.Turn(ctx, convID, new("go"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	pending, _ := pause.ListPending(ctx, convID)
	if len(pending) != 2 {
		t.Fatalf("want 2 pending, got %d", len(pending))
	}
	batch := map[string]ResponseInput{
		pending[0].Token: {Action: askuser.ActionAccept, Content: "ans-a"},
		pending[1].Token: {Action: askuser.ActionAccept, Content: "ans-b"},
	}
	if _, err := r.SubmitAnswers(ctx, batch); err != nil {
		t.Fatalf("first batch: %v", err)
	}
	countAfterFirst, _ := conv.CountTurns(ctx, convID)

	// Re-submit the SAME batch: both tokens are already resolved → the claim-all step fails
	// and NO second set of answer turns is appended.
	_, dupErr := r.SubmitAnswers(ctx, batch)
	if !errors.Is(dupErr, askuser.ErrPauseNotFound) {
		t.Fatalf("a duplicate batch must return ErrPauseNotFound, got %v", dupErr)
	}
	countAfterDup, _ := conv.CountTurns(ctx, convID)
	if countAfterDup != countAfterFirst {
		t.Fatalf("a duplicate batch must NOT append a second set of answers: %d -> %d", countAfterFirst, countAfterDup)
	}

	// Exactly one answer per pause + wire-valid rehydration.
	hist, err := conv.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatal(err)
	}
	assertWireValid(t, hist)
	if pause.unresolvedCount(convID) != 0 {
		t.Fatalf("both pauses must be resolved after the batch, %d remain", pause.unresolvedCount(convID))
	}
}

// TestSubmitAnswers_PartiallyResolvedBatchAppendsNothing proves the batch is
// all-or-nothing at the claim boundary: if ONE token in the batch is already resolved, the
// whole batch fails and appends NO answer turns (not even for the still-pending token) —
// the pre-34-06 inject-first path would have appended answers before the batch claim.
func TestResumeBatch_PartiallyResolvedAppendsNothing(t *testing.T) {
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(
			askUserCall("call-1", "A?", "clarification"),
			askUserCall("call-2", "B?", "clarification"),
		),
	)
	r, conv, pause := newTestRunner(t, client)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)
	if _, err := drain(r.Turn(ctx, convID, new("go"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	pending, _ := pause.ListPending(ctx, convID)
	if len(pending) != 2 {
		t.Fatalf("want 2 pending, got %d", len(pending))
	}
	before, _ := conv.CountTurns(ctx, convID)

	// Resolve ONE pause out-of-band, then submit a batch covering BOTH.
	if _, err := r.SubmitAnswer(ctx, pending[0].Token, ResponseInput{Action: askuser.ActionAccept, Content: "solo"}); err != nil {
		t.Fatalf("out-of-band single resume: %v", err)
	}
	afterSolo, _ := conv.CountTurns(ctx, convID)

	_, err := r.SubmitAnswers(ctx, map[string]ResponseInput{
		pending[0].Token: {Action: askuser.ActionAccept, Content: "a"},
		pending[1].Token: {Action: askuser.ActionAccept, Content: "b"},
	})
	if !errors.Is(err, askuser.ErrPauseNotFound) {
		t.Fatalf("a batch with an already-resolved token must fail with ErrPauseNotFound, got %v", err)
	}
	afterBatch, _ := conv.CountTurns(ctx, convID)
	if afterBatch != afterSolo {
		t.Fatalf("a failed batch claim must append NO turns: %d (pre-batch) -> %d (post-batch)", afterSolo, afterBatch)
	}
	if afterSolo != before+1 {
		t.Fatalf("the out-of-band single resume should have appended exactly one answer, %d -> %d", before, afterSolo)
	}
	// pending[1] stays pending — the failed batch never claimed it.
	if pause.unresolvedCount(convID) != 1 {
		t.Fatalf("the still-pending pause must remain unresolved after the failed batch, %d unresolved", pause.unresolvedCount(convID))
	}
}
