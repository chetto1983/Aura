//go:build db_integration

package steer

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
)

func workerSteerContext(t *testing.T, owner, operation string) context.Context {
	t.Helper()
	fp, err := idempotency.FingerprintTyped(map[string]string{"operation": operation})
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := idempotency.WithOperation(identityctx.WithIdentityID(t.Context(), owner), idempotency.Operation{
		Key: idempotency.OperationKey{IdentityID: owner, Scope: idempotency.ScopeHTTPMutation, Key: operation}, Fingerprint: fp,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func TestWorkerSteersStayInTheirExecutionAndPreserveLogicalRetry(t *testing.T) {
	pool := steerDisposablePool(t)
	owner, conv := seedIdentityAndConversation(t, pool)
	store := NewPostgresStore(pool, Config{Max: 8, MaxBytes: 4096})
	runA, runB := "run-"+uuid.NewString(), "run-"+uuid.NewString()
	first := workerSteerContext(t, owner, "one")
	for range 2 {
		if err := store.PushWorker(first, conv, "w1", runA, "first correction"); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.PushWorker(workerSteerContext(t, owner, "two"), conv, "w1", runA, "second correction"); err != nil {
		t.Fatal(err)
	}
	if err := store.PushWorker(workerSteerContext(t, owner, "three"), conv, "w2", runB, "sibling correction"); err != nil {
		t.Fatal(err)
	}
	if err := store.Push(conv, "cockpit", "parent correction"); err != nil {
		t.Fatal(err)
	}
	if got := store.Drain(conv); len(got) != 1 || got[0].Text != "parent correction" {
		t.Fatalf("parent drain crossed worker scope: %+v", got)
	}
	newAttempt, err := store.ForWorker(first, conv, "w1", "run-"+uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if got := newAttempt.Drain(conv + "-swarm-w1"); len(got) != 0 {
		t.Fatalf("new attempt consumed previous corrections: %+v", got)
	}
	inbox, err := store.ForWorker(first, conv, "w1", runA)
	if err != nil {
		t.Fatal(err)
	}
	if got := inbox.Drain(conv + "-swarm-w2"); len(got) != 0 {
		t.Fatal("mismatched worker session was accepted")
	}
	got := inbox.Drain(conv + "-swarm-w1")
	if len(got) != 2 || got[0].Text != "first correction" || got[1].Text != "second correction" {
		t.Fatalf("FIFO/retry failed: %+v", got)
	}
	if len(inbox.Drain(conv+"-swarm-w1")) != 0 {
		t.Fatal("corrections applied twice")
	}
	sibling, err := store.ForWorker(first, conv, "w2", runB)
	if err != nil {
		t.Fatal(err)
	}
	if got := sibling.Drain(conv + "-swarm-w2"); len(got) != 1 || got[0].Text != "sibling correction" {
		t.Fatalf("sibling lost its correction: %+v", got)
	}
	history, err := store.WorkerHistory(first, conv, "w1")
	if err != nil || len(history) != 2 {
		t.Fatalf("history: %+v %v", history, err)
	}
	for _, receipt := range history {
		if receipt.Status != "applied" || receipt.AppliedAt == nil {
			t.Fatalf("receipt does not prove application: %+v", receipt)
		}
	}
	foreign := workerSteerContext(t, uuid.NewString(), "foreign")
	if err := store.PushWorker(foreign, conv, "w1", runA, "foreign correction"); err == nil {
		t.Fatal("foreign owner inserted a correction")
	}
	if got, err := store.WorkerHistory(foreign, conv, "w1"); err != nil || len(got) != 0 {
		t.Fatalf("foreign history leaked: %+v %v", got, err)
	}
}

func TestWorkerInboxCloseSettlesUnappliedCorrections(t *testing.T) {
	pool := steerDisposablePool(t)
	owner, conv := seedIdentityAndConversation(t, pool)
	store := NewPostgresStore(pool, Config{Max: 8, MaxBytes: 4096})
	run := "run-" + uuid.NewString()
	ctx := workerSteerContext(t, owner, "applied")
	inbox, err := store.ForWorker(ctx, conv, "w1", run)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PushWorker(ctx, conv, "w1", run, "applied"); err != nil {
		t.Fatal(err)
	}
	if len(inbox.Drain(conv+"-swarm-w1")) != 1 {
		t.Fatal("missing correction")
	}
	if err := store.PushWorker(workerSteerContext(t, owner, "late"), conv, "w1", run, "late"); err != nil {
		t.Fatal(err)
	}
	if err := inbox.Close(); err != nil {
		t.Fatal(err)
	}
	if err := inbox.Close(); err != nil {
		t.Fatal(err)
	}
	history, err := store.WorkerHistory(ctx, conv, "w1")
	if err != nil || len(history) != 2 {
		t.Fatalf("history: %+v %v", history, err)
	}
	if history[0].Status != "applied" || history[1].Status != "rejected" || history[1].Reason != "worker_run_ended" {
		t.Fatalf("incorrect settlement: %+v", history)
	}
	if len(inbox.Drain(conv+"-swarm-w1")) != 0 {
		t.Fatal("closed execution consumed a late correction")
	}
}

func TestConcurrentSteerAdmissionCannotExceedConversationCap(t *testing.T) {
	pool := steerDisposablePool(t)
	owner, conv := seedIdentityAndConversation(t, pool)
	store := NewPostgresStore(pool, Config{Max: 3, MaxBytes: 4096})
	contexts := make([]context.Context, 12)
	for i := range contexts {
		contexts[i] = workerSteerContext(t, owner, uuid.NewString())
	}
	run := "run-" + uuid.NewString()
	start := make(chan struct{})
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for _, ctx := range contexts {
		wg.Go(func() {
			<-start
			err := store.PushWorker(ctx, conv, "w1", run, "correction")
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, ErrQueueFull) {
				t.Errorf("unexpected refusal: %v", err)
			}
		})
	}
	close(start)
	wg.Wait()
	if accepted.Load() != 3 {
		t.Fatalf("accepted %d, want exactly cap=3", accepted.Load())
	}
}
