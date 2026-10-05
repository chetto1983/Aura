//go:build db_integration

// Integration test for DeleteSettledOneShots against the real schema and the aura_app role.
// The role has no DELETE on agent_job_runs or pending_notifications: the delete reaches them
// only through the ON DELETE CASCADE foreign keys (0009, 0013), which is the claim this test
// exists to prove. Run via:
//
//	go test -tags db_integration -race -run TestDeleteSettledOneShots ./internal/cron -count=1
package cron

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fireOneShot creates an `at` task, marks it fired the way the tick does (next_run_at NULL),
// and records one run in runStatus. It returns the task and run ids.
func fireOneShot(t *testing.T, ctx context.Context, s *Store, runStatus string) (string, string) {
	t.Helper()
	taskID := makeOneShot(t, ctx, s)
	if err := s.UpdateNextRunAt(ctx, taskID, time.Time{}); err != nil {
		t.Fatalf("UpdateNextRunAt: %v", err)
	}
	run, err := s.InsertRun(ctx, taskID, 1)
	if err != nil {
		t.Fatalf("InsertRun: %v", err)
	}
	if runStatus != "running" {
		if err := s.CompleteRun(ctx, CompleteRunParams{
			RunID: run.ID, Status: runStatus, Summary: "fired", CompletedHash: uuid.NewString(),
		}); err != nil {
			t.Fatalf("CompleteRun: %v", err)
		}
	}
	return taskID, run.ID
}

// makeOneShot creates an `at` task that has not fired yet.
func makeOneShot(t *testing.T, ctx context.Context, s *Store) string {
	t.Helper()
	spec, err := ParseSchedule("at", "", 0, time.Now().Add(time.Hour), "Europe/Rome")
	if err != nil {
		t.Fatalf("ParseSchedule: %v", err)
	}
	task, err := s.CreateTask(ctx, CreateTaskParams{
		Kind: KindReminder, Spec: spec, Payload: []byte(`{"text":"one-shot"}`),
		NextRunAt: spec.RunAt, NotifyRoute: "whatsapp",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() { cleanupTask(t, s.pool, task.ID) })
	return task.ID
}

func owesNotification(t *testing.T, ctx context.Context, s *Store, runID, status string, attempts int) string {
	t.Helper()
	n, err := s.InsertPendingNotification(ctx, InsertPendingNotificationParams{
		RunID: runID, NotifyRoute: "whatsapp", Body: "one-shot", Attempts: attempts,
		Status: status, IdentityID: "local",
	})
	if err != nil {
		t.Fatalf("InsertPendingNotification: %v", err)
	}
	return n.ID
}

func taskGone(t *testing.T, ctx context.Context, s *Store, id string) bool {
	t.Helper()
	_, err := s.GetTask(ctx, id)
	if err != nil && !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("GetTask %s: %v", id, err)
	}
	return errors.Is(err, ErrTaskNotFound)
}

func rowCount(t *testing.T, ctx context.Context, s *Store, query, id string) int {
	t.Helper()
	var n int
	if err := s.pool.QueryRow(ctx, query, id).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

func TestDeleteSettledOneShots(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s := New(pool)
	const bound = 3

	delivered, deliveredRun := fireOneShot(t, ctx, s, "completed")
	owesNotification(t, ctx, s, deliveredRun, "delivered", 1)
	exhausted, exhaustedRun := fireOneShot(t, ctx, s, "failed")
	exhaustedNote := owesNotification(t, ctx, s, exhaustedRun, "failed", bound)

	running, _ := fireOneShot(t, ctx, s, "running")
	pending, pendingRun := fireOneShot(t, ctx, s, "completed")
	owesNotification(t, ctx, s, pendingRun, "pending", 0)
	retrying, retryingRun := fireOneShot(t, ctx, s, "completed")
	owesNotification(t, ctx, s, retryingRun, "failed", bound-1)

	unfired := makeOneShot(t, ctx, s)
	recurring, _ := seedTaskWithRuns(t, ctx, s, time.Now().UTC(), 1)
	if err := s.UpdateNextRunAt(ctx, recurring, time.Time{}); err != nil {
		t.Fatalf("UpdateNextRunAt recurring: %v", err)
	}

	n, err := s.DeleteSettledOneShots(ctx, bound)
	if err != nil {
		t.Fatalf("DeleteSettledOneShots: %v", err)
	}
	// At least, not exactly: the database is shared, and another fired one-shot may be settled.
	if n < 2 {
		t.Fatalf("deleted %d tasks, want at least the 2 settled ones", n)
	}
	for name, id := range map[string]string{"delivered": delivered, "exhausted retries": exhausted} {
		if !taskGone(t, ctx, s, id) {
			t.Errorf("%s one-shot %s survived the delete", name, id)
		}
		if c := rowCount(t, ctx, s, `SELECT count(*) FROM aura.agent_job_runs WHERE task_id = $1::uuid`, id); c != 0 {
			t.Errorf("%s one-shot left %d runs behind: the cascade did not reach agent_job_runs", name, c)
		}
	}
	if c := rowCount(t, ctx, s, `SELECT count(*) FROM aura.pending_notifications WHERE id = $1::uuid`, exhaustedNote); c != 0 {
		t.Error("the exhausted notification survived: the cascade did not reach pending_notifications")
	}
	kept := map[string]string{
		"still running": running, "notification pending": pending, "retry still owed": retrying,
		"not fired yet": unfired, "recurring": recurring,
	}
	for name, id := range kept {
		if taskGone(t, ctx, s, id) {
			t.Errorf("%s task %s was deleted", name, id)
		}
	}
}
