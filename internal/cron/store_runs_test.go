//go:build db_integration

// Integration tests for the GOV-03 paginated run-history query (ListRunsForTask).
// Requires a running Postgres with the migrations applied. Run via:
//
//	go test -tags db_integration -race -run TestListRunsForTask ./internal/cron -count=1
//
// No-skip-as-green: migratedPool's envOrSkip (store_test.go) t.Fatals under $CI when
// the DSN is unset. The package goleak gate (main_test.go) covers leaked pgx pools.
package cron

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// seedTaskWithRuns creates one task and n runs whose started_at is staggered (run i
// started i minutes before now), so newest-first ordering is deterministic. Returns
// the task id and the run ids in chronological (oldest→newest) order.
func seedTaskWithRuns(t *testing.T, ctx context.Context, s *Store, now time.Time, n int) (string, []string) {
	t.Helper()
	spec, err := ParseSchedule("every", "", 5, time.Time{}, "Europe/Rome")
	if err != nil {
		t.Fatalf("ParseSchedule: %v", err)
	}
	task, err := s.CreateTask(ctx, CreateTaskParams{Kind: KindReminder, Spec: spec, StepBudget: 8, NextRunAt: now})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() { cleanupTask(t, s.pool, task.ID) })

	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		run, err := s.InsertRun(ctx, task.ID, 8)
		if err != nil {
			t.Fatalf("InsertRun %d: %v", i, err)
		}
		// Stagger started_at: oldest first. Run i started (n-i) minutes ago.
		started := now.Add(-time.Duration(n-i) * time.Minute)
		if _, err := s.pool.Exec(ctx, "UPDATE aura.agent_job_runs SET started_at = $2 WHERE id = $1", run.ID, started); err != nil {
			t.Fatalf("stagger started_at for run %d: %v", i, err)
		}
		ids = append(ids, run.ID)
	}
	return task.ID, ids
}

// TestListRunsForTask_NewestFirst proves ListRunsForTask returns a task's runs in
// started_at DESC order under the default limit.
func TestListRunsForTask_NewestFirst(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	ctx := context.Background()
	now := time.Now().UTC()

	taskID, chrono := seedTaskWithRuns(t, ctx, s, now, 3)

	runs, err := s.ListRunsForTask(ctx, taskID, 0, 0) // limit<=0 → default 25
	if err != nil {
		t.Fatalf("ListRunsForTask: %v", err)
	}
	if len(runs) != 3 {
		t.Fatalf("ListRunsForTask: want 3 runs, got %d", len(runs))
	}
	// Newest-first: the last-created (chrono[2], started 1m ago) comes first.
	wantOrder := []string{chrono[2], chrono[1], chrono[0]}
	for i, got := range runs {
		if got.ID != wantOrder[i] {
			t.Errorf("run[%d]: want %s, got %s (not started_at DESC)", i, wantOrder[i], got.ID)
		}
		if got.TaskID != taskID {
			t.Errorf("run[%d] task_id: want %s, got %s", i, taskID, got.TaskID)
		}
	}
	// Strictly descending started_at.
	for i := 1; i < len(runs); i++ {
		if runs[i].StartedAt.After(runs[i-1].StartedAt) {
			t.Errorf("started_at not DESC at %d: %s after %s", i, runs[i].StartedAt, runs[i-1].StartedAt)
		}
	}
}

// TestListRunsForTask_LimitOffset proves the wrapper honors limit and offset for
// board pagination.
func TestListRunsForTask_LimitOffset(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	ctx := context.Background()
	now := time.Now().UTC()

	taskID, chrono := seedTaskWithRuns(t, ctx, s, now, 5)
	newestFirst := []string{chrono[4], chrono[3], chrono[2], chrono[1], chrono[0]}

	// First page: limit 2 → the two newest.
	page1, err := s.ListRunsForTask(ctx, taskID, 2, 0)
	if err != nil {
		t.Fatalf("ListRunsForTask page1: %v", err)
	}
	if len(page1) != 2 || page1[0].ID != newestFirst[0] || page1[1].ID != newestFirst[1] {
		t.Fatalf("page1: want [%s %s], got %d rows %v", newestFirst[0], newestFirst[1], len(page1), runIDs(page1))
	}

	// Second page: limit 2, offset 2 → the next two.
	page2, err := s.ListRunsForTask(ctx, taskID, 2, 2)
	if err != nil {
		t.Fatalf("ListRunsForTask page2: %v", err)
	}
	if len(page2) != 2 || page2[0].ID != newestFirst[2] || page2[1].ID != newestFirst[3] {
		t.Fatalf("page2 (offset 2): want [%s %s], got %d rows %v", newestFirst[2], newestFirst[3], len(page2), runIDs(page2))
	}
}

// TestListRunsForTask_EmptyAndBadID proves a task with no runs yields an empty (non-
// nil) slice, and a malformed task id is a wrapped error (not a panic).
func TestListRunsForTask_EmptyAndBadID(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	ctx := context.Background()

	// A random (valid) UUID with no runs → empty slice, no error.
	rows, err := s.ListRunsForTask(ctx, uuid.Must(uuid.NewV7()).String(), 10, 0)
	if err != nil {
		t.Fatalf("ListRunsForTask(no runs): %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("ListRunsForTask(no runs): want empty, got %d", len(rows))
	}

	if _, err := s.ListRunsForTask(ctx, "not-a-uuid", 10, 0); err == nil {
		t.Error("ListRunsForTask(bad uuid): want error, got nil")
	}
}

func runIDs(runs []Run) []string {
	out := make([]string, 0, len(runs))
	for _, r := range runs {
		out = append(out, r.ID)
	}
	return out
}
