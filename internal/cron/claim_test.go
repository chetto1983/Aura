//go:build db_integration

// Integration tests for the held-conn advisory-lock claim (D-03/D-04). These prove
// SC#1 (singleton under concurrency) + the SC#2 idempotency/failover half against a
// live Postgres. Run via:
//
//	go test -tags db_integration -race -run TestClaim ./internal/cron -count=1
//
// migratedPool/envOrSkip/cleanupTask come from store_test.go (same build tag).
package cron

import (
	"context"
	"errors"
	"testing"
	"time"
)

// newSchedulerForTest builds a Scheduler wired to the live pool with a frozen clock
// (the claim path itself does not read Now, but downstream tests reuse this).
func newSchedulerForTest(t *testing.T, frozen time.Time) *Scheduler {
	t.Helper()
	pool := migratedPool(t)
	store := New(pool)
	return NewScheduler(pool, store, SchedulerConfig{
		Now:           func() time.Time { return frozen },
		MaxConcurrent: 2,
		TickInterval:  time.Second,
	})
}

// seedDueTask inserts an already-due every-5m task and returns it.
func seedDueTask(t *testing.T, s *Scheduler, now time.Time) Task {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	spec, err := ParseSchedule("every", "", 5, time.Time{}, "Europe/Rome")
	if err != nil {
		t.Fatalf("ParseSchedule: %v", err)
	}
	task, err := s.store.CreateTask(ctx, CreateTaskParams{
		Kind:       KindReminder,
		Spec:       spec,
		StepBudget: 8,
		NextRunAt:  now.Add(-time.Minute), // already due
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() { cleanupTask(t, s.pool, task.ID) })
	return task
}

func TestClaimSkipLocked_Singleton(t *testing.T) {
	now := time.Now().UTC()
	s := newSchedulerForTest(t, now)
	task := seedDueTask(t, s, now)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// First claim wins the advisory lock and holds its conn.
	c1, err := s.claim(ctx, task)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if c1 == nil || c1.RunID == "" || c1.Conn() == nil {
		t.Fatalf("first claim returned an empty Claim: %+v", c1)
	}

	// Second claim on the same task must lose the lock → ErrAlreadyRunning, conn
	// Released (no leak). We do not assert pool internals; goleak + the survivor
	// re-acquire below prove the conn was returned clean.
	c2, err := s.claim(ctx, task)
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second claim = %v, want ErrAlreadyRunning", err)
	}
	if c2 != nil {
		t.Errorf("second claim returned a non-nil Claim: %+v", c2)
	}

	// After the winner releases, a fresh claim re-acquires (lock available again).
	c1.release(ctx)
	c3, err := s.claim(ctx, task)
	if err != nil {
		t.Fatalf("re-claim after release: %v", err)
	}
	c3.release(ctx)
}

func TestClaimSurvivorReacquiresAfterConnClose(t *testing.T) {
	now := time.Now().UTC()
	s := newSchedulerForTest(t, now)
	task := seedDueTask(t, s, now)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Winner holds the lock; a contender is blocked.
	winner, err := s.claim(ctx, task)
	if err != nil {
		t.Fatalf("winner claim: %v", err)
	}
	if _, err := s.claim(ctx, task); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("contender during held lock = %v, want ErrAlreadyRunning", err)
	}

	// Simulate the winner's worker dying WITHOUT a graceful unlock: hijack the held
	// conn and hard-close its underlying pgconn so Postgres ends the session and
	// auto-releases the advisory lock (the chaos-failover semantics, SC#2).
	hw := winner.conn.Hijack()
	if err := hw.Close(ctx); err != nil {
		t.Fatalf("hard-close hijacked conn: %v", err)
	}

	// A survivor now re-acquires the same task — the session-end auto-release made
	// the lock available again. This is the chaos test at the unit level.
	deadline := time.Now().Add(5 * time.Second)
	var survivor *Claim
	for {
		survivor, err = s.claim(ctx, task)
		if err == nil {
			break
		}
		if !errors.Is(err, ErrAlreadyRunning) {
			t.Fatalf("survivor claim: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("survivor never re-acquired after winner's session ended")
		}
		time.Sleep(50 * time.Millisecond)
	}
	survivor.release(ctx)
}

func TestClaimIdempotentCompletion(t *testing.T) {
	now := time.Now().UTC()
	s := newSchedulerForTest(t, now)
	task := seedDueTask(t, s, now)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c1, err := s.claim(ctx, task)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	defer c1.release(ctx)

	const hash = "claim-idem-hash"
	if err := s.store.CompleteRun(ctx, CompleteRunParams{
		RunID: c1.RunID, Status: "completed", Summary: "ok", CompletedHash: hash,
	}); err != nil {
		t.Fatalf("first CompleteRun: %v", err)
	}

	// A second completion reusing the same completed_with_hash trips the UNIQUE
	// constraint and is swallowed as ErrAlreadyRunning (SC#2 idempotency half).
	run2, err := s.store.InsertRun(ctx, task.ID, task.StepBudget)
	if err != nil {
		t.Fatalf("InsertRun #2: %v", err)
	}
	err = s.store.CompleteRun(ctx, CompleteRunParams{
		RunID: run2.ID, Status: "completed", Summary: "dup", CompletedHash: hash,
	})
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("duplicate completion hash = %v, want ErrAlreadyRunning", err)
	}
}
