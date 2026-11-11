//go:build db_integration

package cron

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// parkedTask inserts a task in status, at next, created at created.
func parkedTask(t *testing.T, pool *pgxpool.Pool, kind TaskKind, scheduleKind, status string, next, created time.Time) string {
	t.Helper()
	id := uuid.NewString()
	every := any(nil)
	if scheduleKind == "every" {
		every = 30
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO aura.scheduler_tasks
(id, kind, schedule_kind, every_minutes, run_at, tz, payload, status, next_run_at, notify_route, identity_id, created_at)
VALUES ($1, $2, $3, $4, $5, 'Europe/Rome', '{"goal":"g"}', $6, $5, 'none', $7, $8)`,
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
