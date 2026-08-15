//go:build db_integration

// The per-tick orphan sweep. Boot recovery is covered by recover_test.go; this covers
// the case a boot-only scan structurally cannot see.
//
//	go test -tags db_integration -race -run TestTickOnce ./internal/cron -count=1
package cron

import (
	"context"
	"testing"
	"time"
)

// TestTickOnceRepudiatesARunThatWentStaleAfterBoot pins the fix for a gap measured on
// the live stack on 2026-08-15: a run whose worker died stayed 'running' across TWO
// restarts, because at each boot its heartbeat was only 20s and then 82s stale — both
// correctly under staleRecoverySeconds — and the scan ran only at boot. A third restart
// finally repudiated it.
//
// The consequence is not cosmetic: D-06 gates a deploy on "zero in-progress runs", and
// on a daemon that stays up for weeks that check would never clear on its own, because
// nothing rescans after boot.
//
// This asserts the tick does what boot cannot: a run that is FRESH when the daemon
// starts, and goes stale while it keeps running, is repudiated by a later tick.
func TestTickOnceRepudiatesARunThatWentStaleAfterBoot(t *testing.T) {
	now := time.Now().UTC()
	s := newSchedulerForTest(t, now)
	task := seedDueTask(t, s, now)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	run, err := s.store.InsertRun(ctx, task.ID, task.StepBudget)
	if err != nil {
		t.Fatalf("InsertRun: %v", err)
	}

	// Boot: the run is fresh, so recovery correctly leaves it alone. This is the state
	// the old boot-only scan was stuck in.
	if err := s.recoverOrphans(ctx); err != nil {
		t.Fatalf("boot recoverOrphans: %v", err)
	}
	if got := runStatus(t, ctx, s, run.ID); got != "running" {
		t.Fatalf("after boot scan, fresh run status = %q, want running (untouched)", got)
	}

	// The worker dies. Time passes; the heartbeat goes stale WHILE the daemon keeps
	// running, so no further boot ever happens to notice.
	backdateHeartbeat(t, ctx, s, run.ID, 5*time.Minute)

	// Before the fix this was the end of the story and the row stayed 'running'
	// indefinitely. Now a tick repudiates it.
	s.tickOnce(ctx)

	if got := runStatus(t, ctx, s, run.ID); got != "unknown_recovery" {
		t.Errorf("after a tick, stale run status = %q, want unknown_recovery — "+
			"the per-tick sweep did not fire, so a dead run would hold 'running' until "+
			"the next restart and D-06's drain check would never clear", got)
	}
}

// TestTickOnceLeavesAHealthyRunAlone is the control: the sweep must repudiate only what
// is actually stale. Without this, the test above would pass just as well against a tick
// that marked every running row, which would repudiate live work on every deploy.
func TestTickOnceLeavesAHealthyRunAlone(t *testing.T) {
	now := time.Now().UTC()
	s := newSchedulerForTest(t, now)
	task := seedDueTask(t, s, now)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	run, err := s.store.InsertRun(ctx, task.ID, task.StepBudget)
	if err != nil {
		t.Fatalf("InsertRun: %v", err)
	}

	s.tickOnce(ctx)

	if got := runStatus(t, ctx, s, run.ID); got != "running" {
		t.Errorf("healthy run status = %q after a tick, want running — the sweep "+
			"repudiated work that was still alive", got)
	}
}

// TestTickOnceSweepIsIdempotent proves repeating it every tick costs nothing: an
// already-repudiated row no longer matches the status='running' scan, so it is not
// re-marked and cannot churn.
func TestTickOnceSweepIsIdempotent(t *testing.T) {
	now := time.Now().UTC()
	s := newSchedulerForTest(t, now)
	task := seedDueTask(t, s, now)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	run, err := s.store.InsertRun(ctx, task.ID, task.StepBudget)
	if err != nil {
		t.Fatalf("InsertRun: %v", err)
	}
	backdateHeartbeat(t, ctx, s, run.ID, 5*time.Minute)

	for i := range 3 {
		s.tickOnce(ctx)
		if got := runStatus(t, ctx, s, run.ID); got != "unknown_recovery" {
			t.Fatalf("after tick %d, status = %q, want a stable unknown_recovery", i+1, got)
		}
	}
}
