//go:build db_integration

package cron

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestApproveTaskChecksOneShotDeadlineAtTheStore(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	ctx := context.Background()
	now := time.Now()
	for _, tc := range []struct {
		name   string
		kind   string
		offset time.Duration
		want   string
	}{
		{"past one-shot", "at", -time.Minute, "pending_approval"},
		{"due one-shot", "at", 0, "pending_approval"},
		{"future one-shot", "at", time.Hour, "active"},
		{"past recurring", "every", -time.Hour, "active"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := parkedTask(t, pool, KindAgentJob, tc.kind, "pending_approval", now.Add(tc.offset), now)
			err := s.ApproveTask(ctx, id)
			if tc.want == "pending_approval" && !errors.Is(err, ErrTaskNotFound) || tc.want == "active" && err != nil {
				t.Fatalf("ApproveTask = %v, want status %s", err, tc.want)
			}
			task, err := s.GetTask(ctx, id)
			if err != nil || task.Status != tc.want {
				t.Fatalf("task status = %q, %v, want %q", task.Status, err, tc.want)
			}
		})
	}
}

func TestRescheduleCannotReviveALapsedPendingOneShot(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	ctx := context.Background()
	now := time.Now()
	edit := UpdateTaskParams{Spec: ScheduleSpec{Kind: KindEvery, EveryMinutes: 60, TZ: "UTC"}, NextRunAt: now.Add(time.Hour), NotifyRoute: "stdout"}
	for _, tc := range []struct {
		status, kind string
		offset       time.Duration
		editable     bool
	}{
		{"pending_approval", "at", -time.Minute, false},
		{"pending_approval", "at", time.Hour, true},
		{"pending_approval", "every", -time.Minute, true},
		{"active", "at", -time.Minute, true},
		{"paused", "every", -time.Minute, true},
	} {
		id := parkedTask(t, pool, KindAgentJob, tc.kind, tc.status, now.Add(tc.offset), now)
		err := s.UpdateTask(ctx, id, edit)
		if tc.editable && err != nil || !tc.editable && !errors.Is(err, ErrTaskNotFound) {
			t.Errorf("rescheduling %+v: %v", tc, err)
		}
	}
}

func TestAnUncommittedRescheduleCannotCloseTheOldPause(t *testing.T) {
	pool := migratedPool(t)
	ctx := context.Background()
	now := time.Now()
	id := parkedTask(t, pool, KindAgentJob, "at", "pending_approval", now.Add(-time.Minute), now)
	edit, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer edit.Rollback(ctx) //nolint:errcheck // The edit may have committed.
	if _, err := edit.Exec(ctx, `UPDATE aura.scheduler_tasks SET next_run_at = now() + interval '1 hour' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	closer := &fakeCloser{}
	dispatch := NewDispatch(nil, DispatchDeps{Store: New(pool), LapsedPauseCloser: closer})
	limited, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := dispatch.ExpireLapsedApprovals(limited); err != nil {
		t.Fatal(err)
	}
	if len(closer.answers) != 0 {
		t.Fatalf("expiry answered a rescheduled task's old pause: %v", closer.answers)
	}
	if err := edit.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestRestartCanDeliverTheExpiryToEachNamedRecipient(t *testing.T) {
	pool := migratedPool(t)
	ctx := context.Background()
	past := time.Now().Add(-time.Minute)
	id := parkedTask(t, pool, KindAgentJob, "at", "pending_approval", past, past)
	if _, err := pool.Exec(ctx, `UPDATE aura.scheduler_tasks SET notify_route = 'telegram', telegram_recipients = '{41,72,41}' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	s := New(pool)
	// Stop after settling the task, before any channel is available.
	if err := NewDispatch(nil, DispatchDeps{Store: s}).ExpireLapsedApprovals(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteSettledOneShots(ctx, 8); err != nil {
		t.Fatal(err)
	}
	runs, err := s.ListRunsForTask(ctx, id, 10, 0)
	if err != nil || len(runs) != 1 || runs[0].Status != "failed" || runs[0].CompletedAt.IsZero() {
		t.Fatalf("after restart, failed run = %+v, %v", runs, err)
	}
	rows, err := s.SweepDueNotifications(ctx, 8, 50)
	if err != nil {
		t.Fatal(err)
	}
	var recipients []int64
	for _, row := range rows {
		if row.RunID == runs[0].ID {
			if row.Body != runs[0].Summary || row.NotifyRoute != "telegram" {
				t.Fatalf("incorrect persisted notice: %+v", row)
			}
			recipients = append(recipients, row.TelegramUserID)
		}
	}
	slices.Sort(recipients)
	if !slices.Equal(recipients, []int64{41, 72}) {
		t.Fatalf("durable recipient IDs = %v", recipients)
	}
	// A competing sweep that was opening a replacement conversation cannot redirect
	// the committed notice after cancellation.
	if err := s.SetTaskOrigin(ctx, id, uuid.NewString()); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("stale home reassignment after expiry = %v", err)
	}
	channel := &accountChannel{}
	restarted := NewDispatch(nil, DispatchDeps{Store: New(pool), ChannelDeliverer: channel, Notifier: &captureNotifier{}})
	for range 2 {
		if err := restarted.sweepNotifications(ctx); err != nil {
			t.Fatal(err)
		}
	}
	slices.Sort(channel.calls)
	if !slices.Equal(channel.calls, []string{"text 41", "text 72"}) {
		t.Fatalf("pushes after restart = %v", channel.calls)
	}
}

// parkedTask inserts a task in status, at next, created at created.
func parkedTask(t *testing.T, pool *pgxpool.Pool, kind TaskKind, scheduleKind, status string, next, created time.Time) string {
	t.Helper()
	id := uuid.NewString()
	every := any(nil)
	if scheduleKind == "every" {
		every = 30
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO aura.scheduler_tasks
(id, kind, schedule_kind, every_minutes, run_at, tz, payload, status, next_run_at, notify_route, identity_id, created_at, paused_reason)
VALUES ($1, $2, $3, $4, $5, 'Europe/Rome', '{"goal":"g"}', $6, $5, 'none', $7, $8, CASE WHEN $6 = 'paused' THEN 'operator' END)`,
		id, string(kind), scheduleKind, every, next, status, "lapsed-"+id[:8], created); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM aura.scheduler_tasks WHERE id = $1`, id) })
	return id
}

func TestOnlyLapsedOneShotsAreListed(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	past, future := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	lapsed := parkedTask(t, pool, KindAgentJob, "at", "pending_approval", past, past)
	others := []string{
		parkedTask(t, pool, KindAgentJob, "at", "pending_approval", future, past),
		parkedTask(t, pool, KindAgentJob, "every", "pending_approval", past, past),
		parkedTask(t, pool, KindAgentJob, "at", "active", past, past),
	}
	tasks, err := s.ListLapsedApprovals(context.Background(), 500)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	ids := make([]string, len(tasks))
	for i, task := range tasks {
		ids[i] = task.ID
	}
	if !slices.Contains(ids, lapsed) {
		t.Fatal("the lapsed one-shot was not listed")
	}
	for _, id := range others {
		if slices.Contains(ids, id) {
			t.Fatalf("task %s was listed", id)
		}
	}
	// A failed expiry remains pending for the next tick. It must never mint a fresh
	// approval prompt when the reminder pass runs later in the same tick.
	for _, id := range append(others, lapsed) {
		if err := s.SetTaskOrigin(context.Background(), id, uuid.NewString()); err != nil {
			t.Fatal(err)
		}
	}
	reminders, err := s.ListDueApprovalReminders(context.Background(), time.Now(), 500)
	if err != nil {
		t.Fatal(err)
	}
	reminded := make(map[string]bool)
	for _, task := range reminders {
		reminded[task.ID] = true
	}
	if reminded[lapsed] || !reminded[others[0]] || !reminded[others[1]] {
		t.Fatalf("approval reminders after expiry rollback: %v", reminded)
	}
}

// Part 3 item 6: a task stuck since before the pass existed expires on its first run.
func TestATaskStuckBeforeDeployExpiresOnTheFirstPass(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	threeDaysAgo := time.Now().Add(-72 * time.Hour)
	id := parkedTask(t, pool, KindAgentJob, "at", "pending_approval", threeDaysAgo, threeDaysAgo)
	recorder := &fakeConversationRecorder{}
	d := NewDispatch(nil, DispatchDeps{Store: s, Notifier: &captureNotifier{}, ConversationRecorder: recorder})
	if err := d.ExpireLapsedApprovals(context.Background()); err != nil {
		t.Fatalf("pass: %v", err)
	}
	task, err := s.GetTask(context.Background(), id)
	if err != nil || task.Status != "cancelled" {
		t.Fatalf("task = %v, %v; want cancelled", task.Status, err)
	}
	runs, err := s.ListRunsForTask(context.Background(), id, 10, 0)
	if err != nil || len(runs) != 1 || runs[0].Status != "failed" || !strings.Contains(runs[0].Summary, "was not approved in time and did not run") {
		t.Fatalf("runs = %+v, %v", runs, err)
	}
}
