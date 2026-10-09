//go:build db_integration

// Integration tests for the outage fixes of 2026-10-09 (prd.md §15) against the real schema:
// a job re-armed after a transient failure, and a failed notice that backs off. Run via:
//
//	go test -tags db_integration -race -run 'TestScheduleTransientRetry|TestMarkNotificationFailed' ./internal/cron -count=1
package cron

import (
	"context"
	"testing"
	"time"
)

// dbNow reads the database clock, which every statement under test schedules against.
func dbNow(t *testing.T, ctx context.Context, s *Store) time.Time {
	t.Helper()
	var now time.Time
	if err := s.pool.QueryRow(ctx, `SELECT now()`).Scan(&now); err != nil {
		t.Fatalf("read now(): %v", err)
	}
	return now
}

func transientRetries(t *testing.T, ctx context.Context, s *Store, taskID string) int {
	t.Helper()
	return rowCount(t, ctx, s, `SELECT transient_retries FROM aura.scheduler_tasks WHERE id = $1`, taskID)
}

func assertNear(t *testing.T, what string, got, want time.Time) {
	t.Helper()
	if d := got.Sub(want); d < -5*time.Second || d > 5*time.Second {
		t.Fatalf("%s = %s, want %s (±5s)", what, got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

// TestScheduleTransientRetryRearmsAFiredOneShot walks one fired one-shot through the whole
// retry chain the way the tick does: each retry fire clears next_run_at on claim, so every
// re-arm starts from NULL and takes its own delay. A pending retry keeps the one-shot from
// being deleted as settled, and the chain ends after the last delay.
func TestScheduleTransientRetryRearmsAFiredOneShot(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s := New(pool)
	taskID, _ := fireOneShot(t, ctx, s, "failed")

	for i, delay := range transientRetryDelays {
		now := dbNow(t, ctx, s)
		retry, ok, err := s.ScheduleTransientRetry(ctx, taskID, transientRetryDelays)
		if err != nil || !ok {
			t.Fatalf("retry %d: ok=%v err=%v", i+1, ok, err)
		}
		if retry.Number != i+1 {
			t.Fatalf("retry %d numbered %d", i+1, retry.Number)
		}
		assertNear(t, "retry fire", retry.At, now.Add(delay))
		if i == 0 {
			if n, err := s.DeleteSettledOneShots(ctx, pendingNotificationAttemptBound()); err != nil || taskGone(t, ctx, s, taskID) {
				t.Fatalf("a one-shot waiting for its retry was deleted (n=%d err=%v)", n, err)
			}
		}
		if err := s.UpdateNextRunAt(ctx, taskID, time.Time{}); err != nil {
			t.Fatalf("clear next fire: %v", err)
		}
	}

	if _, ok, err := s.ScheduleTransientRetry(ctx, taskID, transientRetryDelays); err != nil || ok {
		t.Fatalf("a task past its last delay must not be re-armed: ok=%v err=%v", ok, err)
	}
	if got := transientRetries(t, ctx, s, taskID); got != len(transientRetryDelays) {
		t.Fatalf("transient_retries = %d, want %d", got, len(transientRetryDelays))
	}

	if _, err := s.RecordRunOutcome(ctx, taskID, false, 3); err != nil {
		t.Fatalf("RecordRunOutcome: %v", err)
	}
	if got := transientRetries(t, ctx, s, taskID); got != 0 {
		t.Fatalf("a reported outcome must reset transient_retries, got %d", got)
	}
}

// TestScheduleTransientRetryKeepsAnEarlierRegularFire: a recurring task whose next regular fire
// comes before the retry delay keeps that fire, so a retry never postpones the schedule.
func TestScheduleTransientRetryKeepsAnEarlierRegularFire(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s := New(pool)
	spec, _ := ParseSchedule("every", "", 5, time.Time{}, "Europe/Rome")
	regular := time.Now().UTC().Add(time.Minute).Truncate(time.Second)
	task, err := s.CreateTask(ctx, CreateTaskParams{
		Kind: KindAgentJob, Spec: spec, Payload: []byte(`{"goal":"summarize the news"}`),
		NextRunAt: regular, NotifyRoute: "email",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() { cleanupTask(t, pool, task.ID) })

	retry, ok, err := s.ScheduleTransientRetry(ctx, task.ID, transientRetryDelays)
	if err != nil || !ok {
		t.Fatalf("ScheduleTransientRetry: ok=%v err=%v", ok, err)
	}
	if !retry.At.Equal(regular) {
		t.Fatalf("next fire = %s, want the earlier regular fire %s", retry.At, regular)
	}
}

// TestScheduleTransientRetrySkipsAPausedTask: an operator who paused the task during the run
// wins over the retry, and resuming starts the retry count over.
func TestScheduleTransientRetrySkipsAPausedTask(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s := New(pool)
	spec, _ := ParseSchedule("every", "", 5, time.Time{}, "Europe/Rome")
	task, err := s.CreateTask(ctx, CreateTaskParams{
		Kind: KindAgentJob, Spec: spec, Payload: []byte(`{"goal":"summarize the news"}`),
		NextRunAt: time.Now().Add(5 * time.Minute), NotifyRoute: "email",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() { cleanupTask(t, pool, task.ID) })
	if _, ok, err := s.ScheduleTransientRetry(ctx, task.ID, transientRetryDelays); err != nil || !ok {
		t.Fatalf("first retry: ok=%v err=%v", ok, err)
	}

	if err := s.PauseTask(ctx, task.ID); err != nil {
		t.Fatalf("PauseTask: %v", err)
	}
	if _, ok, err := s.ScheduleTransientRetry(ctx, task.ID, transientRetryDelays); err != nil || ok {
		t.Fatalf("a paused task must not be re-armed: ok=%v err=%v", ok, err)
	}
	if _, err := s.ResumeTask(ctx, task.ID, time.Now().UTC()); err != nil {
		t.Fatalf("ResumeTask: %v", err)
	}
	if got := transientRetries(t, ctx, s, task.ID); got != 0 {
		t.Fatalf("a resume must reset transient_retries, got %d", got)
	}
}

// TestMarkNotificationFailedBacksOff pins the notice backoff: each failed retry pushes the next
// one back by 30 s doubled per earlier failure, the sweep leaves the row alone until then, and
// the doubling stops at 2^7 so a large attempt bound cannot overflow the interval.
func TestMarkNotificationFailedBacksOff(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s := New(pool)
	_, runID := fireOneShot(t, ctx, s, "failed")
	id := owesNotification(t, ctx, s, runID, "failed", 0)

	swept := func() bool {
		rows, err := s.SweepDueNotifications(ctx, 100, pendingNotificationSweepLimit)
		if err != nil {
			t.Fatalf("SweepDueNotifications: %v", err)
		}
		for _, r := range rows {
			if r.ID == id {
				return true
			}
		}
		return false
	}
	notifyAfter := func() time.Time {
		var at time.Time
		if err := pool.QueryRow(ctx, `SELECT notify_after FROM aura.pending_notifications WHERE id = $1`, id).Scan(&at); err != nil {
			t.Fatalf("read notify_after: %v", err)
		}
		return at
	}

	if !swept() {
		t.Fatal("a failed notice inserted as due must be swept")
	}
	for _, wait := range []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute} {
		now := dbNow(t, ctx, s)
		if err := s.MarkNotificationFailed(ctx, id, "telegram unreachable"); err != nil {
			t.Fatalf("MarkNotificationFailed: %v", err)
		}
		assertNear(t, "next retry", notifyAfter(), now.Add(wait))
		if swept() {
			t.Fatalf("the sweep picked the notice before its %s wait", wait)
		}
		if _, err := pool.Exec(ctx, `UPDATE aura.pending_notifications SET notify_after = now() WHERE id = $1`, id); err != nil {
			t.Fatalf("make due: %v", err)
		}
		if !swept() {
			t.Fatalf("the notice must be swept once its %s wait is over", wait)
		}
	}

	if _, err := pool.Exec(ctx, `UPDATE aura.pending_notifications SET attempts = 40 WHERE id = $1`, id); err != nil {
		t.Fatalf("raise attempts: %v", err)
	}
	now := dbNow(t, ctx, s)
	if err := s.MarkNotificationFailed(ctx, id, "telegram unreachable"); err != nil {
		t.Fatalf("MarkNotificationFailed at a large attempt count: %v", err)
	}
	assertNear(t, "capped retry", notifyAfter(), now.Add(128*30*time.Second))
}
