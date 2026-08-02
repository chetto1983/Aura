//go:build db_integration

package runner

import (
	"errors"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/askuser"
)

// TestSubmitAnswers_ConcurrentDuplicateBatch_Integration proves LOOP-02/F-004 against live
// Postgres: two concurrent identical batches resolving the SAME pauses → exactly ONE wins
// (all-or-nothing), the loser gets ErrPauseNotFound, the run is deadlock-free (the
// sorted-token claim from 34-05 makes both txs lock rows in the same order under READ
// COMMITTED), and each pause ends with exactly one answer turn (no orphan RoleTool, no
// double-answer).
func TestResumeBatch_ConcurrentDuplicate_Integration(t *testing.T) {
	pool := migratedRunnerPool(t)
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(
			askUserCall("call-1", "A?", "clarification"),
			askUserCall("call-2", "B?", "clarification"),
		),
	)
	r, convStore, _ := newIntegrationRunner(t, pool, client)
	convID := newIntegrationConversation(t, pool, convStore)
	ctx := ownerCtx()

	if _, err := drain(r.Turn(ctx, convID, new("go"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	pending, err := r.PendingFor(ctx, convID)
	if err != nil || len(pending) != 2 {
		t.Fatalf("want 2 pending, got %d (err=%v)", len(pending), err)
	}
	batch := map[string]ResponseInput{
		pending[0].Token: {Action: askuser.ActionAccept, Content: "ans-a"},
		pending[1].Token: {Action: askuser.ActionAccept, Content: "ans-b"},
	}

	// Two goroutines submit the SAME batch concurrently, released together.
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := r.SubmitAnswers(ctx, batch)
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)

	var wins, notFound, other int
	for err := range errs {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, askuser.ErrPauseNotFound):
			notFound++
		default:
			other++
			t.Errorf("unexpected batch error (want nil or ErrPauseNotFound — deadlock-free): %v", err)
		}
	}
	if wins != 1 || notFound != 1 {
		t.Fatalf("concurrent duplicate batch: want exactly 1 win + 1 ErrPauseNotFound, got wins=%d notFound=%d other=%d", wins, notFound, other)
	}

	// Exactly one answer per pause; the rehydrated history is wire-valid.
	hist, err := convStore.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	assertWireValid(t, hist)
	if got := countToolAnswers(hist); got != 2 {
		t.Fatalf("want exactly 2 RoleTool answers (one per pause), got %d (double-answer or orphan)", got)
	}
	remaining, err := r.PendingFor(ctx, convID)
	if err != nil {
		t.Fatalf("PendingFor: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("both pauses must be resolved after the batch, %d remain", len(remaining))
	}
}
