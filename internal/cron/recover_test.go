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

// TestCatchUpMissed_ConsultsReschedulesOnRecovery is the M-g regression: the boot
// catch-up consults each handler's ReschedulesOnRecovery flag. An overdue task whose
// handler does NOT reschedule on recovery (false) is NOT returned as a MissedTask (its
// committed side effect is never replayed) while its cadence is still advanced; a task
// whose handler DOES reschedule (true) is still returned and fires once. Before the fix
// the flag was never read and BOTH fired.
func TestCatchUpMissed_ConsultsReschedulesOnRecovery(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	pool := migratedPool(t)
	store := New(pool)
	// Inject the real flags: skill_ttl_sweep=false (a periodic sweep never replays a
	// missed window), agent_job=true (replays once). Reminder is no longer the false
	// case — fix-plan 1.2 flipped it to true (missed reminders re-fire at catch-up).
	s := NewScheduler(pool, store, SchedulerConfig{
		Now:           func() time.Time { return now },
		MaxConcurrent: 2,
		TickInterval:  time.Hour,
		ReschedulesOnRecovery: func(kind TaskKind) bool {
			return kind == KindAgentJob
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	spec, err := ParseSchedule("every", "", 5, time.Time{}, "Europe/Rome")
	if err != nil {
		t.Fatalf("ParseSchedule: %v", err)
	}
	overdue := now.Add(-time.Hour) // 12 missed every-5m windows

	noReplay, err := store.CreateTask(ctx, CreateTaskParams{Kind: KindSkillTTLSweep, Spec: spec, StepBudget: 8, NextRunAt: overdue})
	if err != nil {
		t.Fatalf("CreateTask skill_ttl_sweep: %v", err)
	}
	t.Cleanup(func() { cleanupTask(t, pool, noReplay.ID) })

	replay, err := store.CreateTask(ctx, CreateTaskParams{Kind: KindAgentJob, Spec: spec, StepBudget: 8, NextRunAt: overdue})
	if err != nil {
		t.Fatalf("CreateTask agent_job: %v", err)
	}
	t.Cleanup(func() { cleanupTask(t, pool, replay.ID) })

	missed, err := s.catchUpMissed(ctx)
	if err != nil {
		t.Fatalf("catchUpMissed: %v", err)
	}

	inMissed := func(id string) bool {
		for i := range missed {
			if missed[i].Task.ID == id {
				return true
			}
		}
		return false
	}

	// The sweep (ReschedulesOnRecovery=false) is NOT re-fired — but its cadence DID
	// advance, so the next window fires normally.
	if inMissed(noReplay.ID) {
		t.Error("a ReschedulesOnRecovery=false task must NOT be auto-re-fired at catch-up (M-g)")
	}
	reloadedNoReplay, err := store.GetTask(ctx, noReplay.ID)
	if err != nil {
		t.Fatalf("GetTask skill_ttl_sweep: %v", err)
	}
	if want := now.Add(5 * time.Minute); !reloadedNoReplay.NextRunAt.Equal(want) {
		t.Errorf("non-rescheduling task next_run_at = %s, want cadence still advanced to %s", reloadedNoReplay.NextRunAt, want)
	}

	// The agent_job (ReschedulesOnRecovery=true) IS re-fired once, carrying MissedSince.
	if !inMissed(replay.ID) {
		t.Error("a ReschedulesOnRecovery=true task must still fire once at catch-up (M-g)")
	}
	for i := range missed {
		if missed[i].Task.ID == replay.ID && !missed[i].MissedSince.Equal(overdue) {
			t.Errorf("agent_job MissedSince = %s, want original overdue %s", missed[i].MissedSince, overdue)
		}
	}
}

// TestStartDispatchesMissedAtBoot is the CR-01 regression: an overdue task at boot
// must actually FIRE once (not just advance next_run_at). It seeds an overdue task,
// wires a recording dispatcher, runs Start until the boot catch-up drains, then
// asserts (a) the task was dispatched exactly once and (b) the catch-up run row
// carries missed_since (the D-18 forensics + SC#3 alert thread). Before the fix
// catchUpMissed's slice was discarded and the missed window was silently dropped.
func TestStartDispatchesMissedAtBoot(t *testing.T) {
	// Truncate to µs: Postgres timestamptz has µs precision; sub-µs digits make the
	// round-trip equality assertion physically unsatisfiable (D-18 forensics contract).
	now := time.Now().UTC().Truncate(time.Microsecond)
	pool := migratedPool(t)
	store := New(pool)
	disp := newRecordingDispatcher(0)
	s := NewScheduler(pool, store, SchedulerConfig{
		Now:           func() time.Time { return now },
		MaxConcurrent: 2,
		TickInterval:  time.Hour, // long: only the boot catch-up should fire in this window
		Dispatch:      disp,
	})

	spec, err := ParseSchedule("every", "", 5, time.Time{}, "Europe/Rome")
	if err != nil {
		t.Fatalf("ParseSchedule: %v", err)
	}
	overdue := now.Add(-time.Hour) // 12 missed every-5m windows → ONE catch-up fire
	task, err := store.CreateTask(ctx(t), CreateTaskParams{
		Kind: KindReminder, Spec: spec, StepBudget: 8, NextRunAt: overdue,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() { cleanupTask(t, pool, task.ID) })

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Start(runCtx) }()

	// The boot catch-up dispatch runs before the (1h) tick fires; poll until the
	// recording dispatcher has seen the task, then cancel to stop the loop.
	deadline := time.After(10 * time.Second)
	for {
		disp.mu.Lock()
		ran := disp.ran[task.ID]
		disp.mu.Unlock()
		if ran > 0 {
			break
		}
		select {
		case <-deadline:
			cancel()
			t.Fatalf("missed task never dispatched at boot (CR-01: catch-up fire dropped)")
		case <-time.After(20 * time.Millisecond):
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("Start returned %v, want nil on graceful cancel", err)
	}

	disp.mu.Lock()
	ran := disp.ran[task.ID]
	disp.mu.Unlock()
	if ran != 1 {
		t.Errorf("missed task dispatched %d times at boot, want exactly 1 (collapse, D-18)", ran)
	}

	// The catch-up run row records the ORIGINAL slipped instant (missed_since), the
	// forensics trail the SC#3 alert + late-notification thread read.
	var missedSince *time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT missed_since FROM aura.agent_job_runs WHERE task_id = $1 ORDER BY started_at DESC LIMIT 1`,
		task.ID).Scan(&missedSince); err != nil {
		t.Fatalf("read missed_since: %v", err)
	}
	if missedSince == nil {
		t.Fatalf("catch-up run missed_since is NULL, want the original overdue instant (D-18)")
	}
	if !missedSince.UTC().Equal(overdue) {
		t.Errorf("missed_since = %s, want original overdue %s", missedSince.UTC(), overdue)
	}
}

// ctx builds a short-lived context for a fixture call (no leak: bounded + cancelled
// via t.Cleanup).
func ctx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	return c
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
