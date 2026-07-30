//go:build db_integration

package runner

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
)

var errInjectedAppendFailure = errors.New("injected append failure after claim")

// TestSubmitAnswer_AppendFailureAfterClaimRollsBack_Integration proves LOOP-03/F-029: an
// append failure AFTER the claim rolls the whole cross-store tx back, leaving the pause
// pending (resumed_at IS NULL) and retryable. The transient failure is injected by running
// the REAL claim (MarkResumedTx) + a forced error in ONE db.WithTx — db.WithTx rolls the
// claim back on any fn error, exactly as a real AppendTurnTx failure inside CommitResume
// would. The retry then succeeds through the real Runner API (the pool-owning committer),
// leaving a durable, wire-valid answer.
func TestResumeSingle_AppendFailureAfterClaimRollsBack_Integration(t *testing.T) {
	pool := migratedRunnerPool(t)
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(askUserCall("call-1", "Which city?", "clarification")),
	)
	r, convStore, pauseStore := newIntegrationRunner(t, pool, client)
	convID := newIntegrationConversation(t, pool, convStore)
	ctx := context.Background()

	// Drive a real Turn to a pause (exercises the atomic flushPause/CommitPause path).
	if _, err := drain(r.Turn(ctx, convID, new("where am I?"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	pending, err := r.PendingFor(ctx, convID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("want 1 pending pause, got %d (err=%v)", len(pending), err)
	}
	token := pending[0].Token

	// Fault injection: the REAL MarkResumedTx claim + a forced append failure in ONE tx.
	answer := askuser.ResumeAnswer{Action: askuser.ActionAccept, Content: "Rome"}
	injErr := db.WithTx(ctx, pool, func(q *sqlc.Queries) error {
		if err := pauseStore.MarkResumedTx(ctx, q, token, answer); err != nil {
			return err
		}
		return errInjectedAppendFailure
	})
	if !errors.Is(injErr, errInjectedAppendFailure) {
		t.Fatalf("want the injected append failure, got %v", injErr)
	}

	// The claim must have rolled back: the pause is still pending AND no answer turn was
	// PERSISTED (probe the DB rows, not LoadHistory — the real store synthesizes a
	// crash-recovery placeholder RoleTool for the still-pending ask_user call, which is not
	// a durable row).
	if ts := resumedAt(t, pool, token); ts != nil {
		t.Fatalf("an append-failure rollback must leave the pause pending (resumed_at IS NULL), got %v", ts)
	}
	if n := countPersistedToolTurns(t, pool, convID); n != 0 {
		t.Fatalf("a rolled-back resume must persist NO answer turn, got %d", n)
	}

	// Retry through the real API (pool-owning committer) — it must succeed now.
	if _, err := r.SubmitAnswer(ctx, token, ResponseInput{Action: askuser.ActionAccept, Content: "Rome"}); err != nil {
		t.Fatalf("retry after the transient failure must succeed: %v", err)
	}
	if ts := resumedAt(t, pool, token); ts == nil {
		t.Fatal("after a successful retry the pause must be resolved (resumed_at set)")
	}
	if n := countPersistedToolTurns(t, pool, convID); n != 1 {
		t.Fatalf("exactly one answer turn must be persisted after the retry, got %d", n)
	}

	// The retried answer is durable + the rehydrated history is wire-valid (the real answer
	// now replaces the synthesized placeholder).
	hist, err := convStore.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	assertWireValid(t, hist)
	if !historyHasToolContentMsgs(hist, "Rome") {
		t.Fatal("the retried answer 'Rome' must be durable as a RoleTool turn")
	}
}

// TestSubmitAnswer_DuplicateResumeIsAtomic_Integration is the single-resume analog of the
// batch atomicity proof: a duplicate accept through the pool-owning committer claims once
// (resumed_at set) and appends exactly one answer; the second submit gets ErrPauseNotFound
// and appends nothing (D-06 conditional-update idempotency, live).
func TestResumeSingle_DuplicateIsAtomic_Integration(t *testing.T) {
	pool := migratedRunnerPool(t)
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(askUserCall("call-1", "Which city?", "clarification")),
	)
	r, convStore, _ := newIntegrationRunner(t, pool, client)
	convID := newIntegrationConversation(t, pool, convStore)
	ctx := context.Background()

	if _, err := drain(r.Turn(ctx, convID, new("where am I?"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	pending, err := r.PendingFor(ctx, convID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("want 1 pending pause, got %d (err=%v)", len(pending), err)
	}
	token := pending[0].Token

	if _, err := r.SubmitAnswer(ctx, token, ResponseInput{Action: askuser.ActionAccept, Content: "Rome"}); err != nil {
		t.Fatalf("first resume: %v", err)
	}
	_, dupErr := r.SubmitAnswer(ctx, token, ResponseInput{Action: askuser.ActionAccept, Content: "Rome again"})
	if !errors.Is(dupErr, askuser.ErrPauseNotFound) {
		t.Fatalf("a duplicate resume must return ErrPauseNotFound, got %v", dupErr)
	}

	hist, err := convStore.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	assertWireValid(t, hist)
	if n := countToolAnswers(hist); n != 1 {
		t.Fatalf("a duplicate resume must NOT append a second answer turn, got %d", n)
	}
}
