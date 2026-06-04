//go:build db_integration

// Integration tests for the dispatch seam against a live Postgres: the dispatcher
// writes the handler summary into agent_job_runs and the completion is idempotent on
// the completed_with_hash key (SC#2). Run via:
//
//	go test -tags db_integration -race -run TestDispatch ./internal/cron -count=1
package cron

import (
	"context"
	"testing"
	"time"
)

func TestDispatchWritesSummaryToRun(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s := New(pool)

	spec, _ := ParseSchedule("every", "", 5, time.Time{}, "Europe/Rome")
	task, err := s.CreateTask(ctx, CreateTaskParams{
		Kind: KindReminder, Spec: spec, Payload: []byte(`{"text":"hydrate"}`),
		NextRunAt: time.Now().Add(5 * time.Minute), NotifyRoute: "stdout",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() { cleanupTask(t, pool, task.ID) })

	run, err := s.InsertRun(ctx, task.ID, 0)
	if err != nil {
		t.Fatalf("InsertRun: %v", err)
	}

	h := &fakeHandler{meta: HandlerMeta{Kind: KindReminder}, summary: "hydrate"}
	var buf stdoutBuf
	d := NewDispatch(map[TaskKind]Handler{KindReminder: h}, DispatchDeps{
		Store: s, Notifier: &compositeNotifier{out: &buf},
	})

	domainTask, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if err := d.Dispatch(ctx, domainTask, &Claim{RunID: run.ID}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	got, err := s.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Status != "completed" || got.Summary != "hydrate" {
		t.Fatalf("run not completed with summary: status=%q summary=%q", got.Status, got.Summary)
	}
	if got.CompletedWithHash != run.ID {
		t.Fatalf("completed_with_hash = %q, want the run id %q", got.CompletedWithHash, run.ID)
	}
}

func TestDispatchCompletionIsIdempotent(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s := New(pool)

	spec, _ := ParseSchedule("every", "", 5, time.Time{}, "Europe/Rome")
	task, err := s.CreateTask(ctx, CreateTaskParams{
		Kind: KindReminder, Spec: spec, NextRunAt: time.Now().Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() { cleanupTask(t, pool, task.ID) })
	run, err := s.InsertRun(ctx, task.ID, 0)
	if err != nil {
		t.Fatalf("InsertRun: %v", err)
	}

	h := &fakeHandler{meta: HandlerMeta{Kind: KindReminder}, summary: "once"}
	var buf stdoutBuf
	d := NewDispatch(map[TaskKind]Handler{KindReminder: h}, DispatchDeps{Store: s, Notifier: &compositeNotifier{out: &buf}})
	domainTask, _ := s.GetTask(ctx, task.ID)

	// A redelivered dispatch of the SAME run hits the completed_with_hash UNIQUE
	// constraint on the second CompleteRun — swallowed as ErrAlreadyRunning by the
	// store, logged by the dispatcher, never a crash (SC#2).
	if err := d.Dispatch(ctx, domainTask, &Claim{RunID: run.ID}); err != nil {
		t.Fatalf("Dispatch #1: %v", err)
	}
	if err := d.Dispatch(ctx, domainTask, &Claim{RunID: run.ID}); err != nil {
		t.Fatalf("Dispatch #2 (redelivery) must not error: %v", err)
	}

	got, err := s.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Summary != "once" {
		t.Fatalf("redelivery must not overwrite the summary, got %q", got.Summary)
	}
}

// stdoutBuf is a tiny io.Writer that discards notification output in the integration
// tests (the delivery path is unit-tested in notify_test.go).
type stdoutBuf struct{}

func (stdoutBuf) Write(p []byte) (int, error) { return len(p), nil }
