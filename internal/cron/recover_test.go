//go:build db_integration

// Integration tests for boot recovery (D-02/D-18): the orphan scan (stale heartbeat
// → unknown_recovery) and missed catch-up (collapse multiple windows to one fire
// with MissedSince). Run via:
//
//	go test -tags db_integration -race -run TestRecover ./internal/cron -count=1
package cron

import (
	"context"
	"testing"
	"time"
)

func TestRecoverOrphans_MarksStaleLeavesFresh(t *testing.T) {
	now := time.Now().UTC()
	s := newSchedulerForTest(t, now)
	task := seedDueTask(t, s, now)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// One run with a stale heartbeat (backdated 5 min) and one fresh run.
	stale, err := s.store.InsertRun(ctx, task.ID, task.StepBudget)
	if err != nil {
		t.Fatalf("InsertRun stale: %v", err)
	}
	backdateHeartbeat(t, ctx, s, stale.ID, 5*time.Minute)

	fresh, err := s.store.InsertRun(ctx, task.ID, task.StepBudget)
	if err != nil {
		t.Fatalf("InsertRun fresh: %v", err)
	}

	if err := s.recoverOrphans(ctx); err != nil {
		t.Fatalf("recoverOrphans: %v", err)
	}

	if got := runStatus(t, ctx, s, stale.ID); got != "unknown_recovery" {
		t.Errorf("stale run status = %q, want unknown_recovery", got)
	}
	if got := runStatus(t, ctx, s, fresh.ID); got != "running" {
		t.Errorf("fresh run status = %q, want running (untouched)", got)
	}
}

func TestCatchUpMissed_CollapsesMultipleWindowsToOne(t *testing.T) {
	// Freeze the clock so the overdue computation is deterministic. The task is an
	// every-5m recurring task whose next_run_at is 1 hour (12 windows) in the past.
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	s := newSchedulerForTest(t, now)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	spec, err := ParseSchedule("every", "", 5, time.Time{}, "Europe/Rome")
	if err != nil {
		t.Fatalf("ParseSchedule: %v", err)
	}
	overdue := now.Add(-1 * time.Hour) // 12 missed every-5m windows
	task, err := s.store.CreateTask(ctx, CreateTaskParams{
		Kind: KindReminder, Spec: spec, StepBudget: 8, NextRunAt: overdue,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() { cleanupTask(t, s.pool, task.ID) })

	missed, err := s.catchUpMissed(ctx)
	if err != nil {
		t.Fatalf("catchUpMissed: %v", err)
	}

	// Exactly ONE catch-up entry for the overdue task, carrying the ORIGINAL missed
	// instant — multiple windows collapsed to a single fire (D-18).
	var got *MissedTask
	for i := range missed {
		if missed[i].Task.ID == task.ID {
			got = &missed[i]
		}
	}
	if got == nil {
		t.Fatalf("overdue task not in catch-up set: %+v", missed)
	}
	if !got.MissedSince.Equal(overdue) {
		t.Errorf("MissedSince = %s, want original overdue %s", got.MissedSince, overdue)
	}

	// next_run_at advanced to a SINGLE future fire (collapse), not re-fired per window.
	reloaded, err := s.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if !reloaded.NextRunAt.After(now) {
		t.Errorf("next_run_at after catch-up = %s, want a future instant > %s", reloaded.NextRunAt, now)
	}
	// every-5m from a frozen now → the next fire is exactly now+5m (one window forward).
	if want := now.Add(5 * time.Minute); !reloaded.NextRunAt.Equal(want) {
		t.Errorf("collapsed next_run_at = %s, want %s (one window forward, not 12)", reloaded.NextRunAt, want)
	}
}

func TestCatchUpMissed_SkipsFutureTasks(t *testing.T) {
	now := time.Now().UTC()
	s := newSchedulerForTest(t, now)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	spec, _ := ParseSchedule("every", "", 5, time.Time{}, "Europe/Rome")
	future := now.Add(30 * time.Minute)
	task, err := s.store.CreateTask(ctx, CreateTaskParams{Kind: KindReminder, Spec: spec, NextRunAt: future})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() { cleanupTask(t, s.pool, task.ID) })

	missed, err := s.catchUpMissed(ctx)
	if err != nil {
		t.Fatalf("catchUpMissed: %v", err)
	}
	for _, m := range missed {
		if m.Task.ID == task.ID {
			t.Errorf("future task wrongly caught up: %+v", m)
		}
	}
}

// backdateHeartbeat pushes a run's last_heartbeat_at into the past so the orphan
// scan's >90s window catches it. Direct UPDATE (test fixture only).
func backdateHeartbeat(t *testing.T, ctx context.Context, s *Scheduler, runID string, age time.Duration) {
	t.Helper()
	secs := age.Seconds()
	if _, err := s.pool.Exec(ctx,
		"UPDATE aura.agent_job_runs SET last_heartbeat_at = now() - make_interval(secs => $2) WHERE id = $1",
		runID, secs); err != nil {
		t.Fatalf("backdate heartbeat: %v", err)
	}
}

// runStatus reads a run's current status off the pool.
func runStatus(t *testing.T, ctx context.Context, s *Scheduler, runID string) string {
	t.Helper()
	run, err := s.store.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	return run.Status
}
