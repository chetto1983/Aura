//go:build db_integration

// Integration tests for the GOV-03 scheduler-management store methods (store_manage.go):
// ListManageableTasks, ApproveTask, RunTaskNow, UpdateTask. Shares the store_test.go
// harness (migratedPool / cleanupTask / envOrSkip). Run via:
//
//	go test -tags db_integration -race ./internal/cron -count=1
package cron

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// makeTask inserts a task at the given status and registers cleanup, returning its id.
func makeTask(t *testing.T, s *Store, ctx context.Context, kind TaskKind, status string) string {
	t.Helper()
	spec, err := ParseSchedule("cron", "30 9 * * *", 0, time.Time{}, "Europe/Rome")
	if err != nil {
		t.Fatalf("ParseSchedule: %v", err)
	}
	next, err := NextRunAt(spec, time.Now())
	if err != nil {
		t.Fatalf("NextRunAt: %v", err)
	}
	created, err := s.CreateTask(ctx, CreateTaskParams{
		Kind: kind, Spec: spec, Payload: []byte(`{"text":"orig"}`),
		NextRunAt: next, NotifyRoute: "stdout", Status: status,
	})
	if err != nil {
		t.Fatalf("CreateTask(%s): %v", status, err)
	}
	t.Cleanup(func() { cleanupTask(t, s.pool, created.ID) })
	return created.ID
}

func TestListManageableTasks_IncludesPendingApproval(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := New(pool)

	active := makeTask(t, s, ctx, KindReminder, "active")
	pending := makeTask(t, s, ctx, KindAgentJob, "pending_approval")

	rows, err := s.ListManageableTasks(ctx)
	if err != nil {
		t.Fatalf("ListManageableTasks: %v", err)
	}
	seen := map[string]string{}
	for _, r := range rows {
		seen[r.ID] = r.Status
	}
	if seen[active] != "active" {
		t.Errorf("active task not listed as active: %v", seen[active])
	}
	if seen[pending] != "pending_approval" {
		t.Errorf("pending_approval task must appear on the board, got %q", seen[pending])
	}
}

func TestApproveTask_FlipsPendingToActive(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := New(pool)

	id := makeTask(t, s, ctx, KindAgentJob, "pending_approval")
	if err := s.ApproveTask(ctx, id); err != nil {
		t.Fatalf("ApproveTask: %v", err)
	}
	got, err := s.GetTask(ctx, id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != "active" {
		t.Fatalf("status after approve = %q, want active", got.Status)
	}
	// A second approve is a no-op miss (no longer pending) → ErrTaskNotFound.
	if err := s.ApproveTask(ctx, id); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("re-approve err = %v, want ErrTaskNotFound", err)
	}
}

func TestRunTaskNow_AdvancesActiveOnly(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := New(pool)

	active := makeTask(t, s, ctx, KindReminder, "active")
	before, err := s.GetTask(ctx, active)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if err := s.RunTaskNow(ctx, active); err != nil {
		t.Fatalf("RunTaskNow: %v", err)
	}
	after, err := s.GetTask(ctx, active)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if !after.NextRunAt.Before(before.NextRunAt) {
		t.Errorf("run-now must pull next_run_at earlier: before=%v after=%v", before.NextRunAt, after.NextRunAt)
	}
	// A pending task is not active → run-now misses (ErrTaskNotFound).
	pending := makeTask(t, s, ctx, KindAgentJob, "pending_approval")
	if err := s.RunTaskNow(ctx, pending); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("run-now on pending err = %v, want ErrTaskNotFound", err)
	}
}

func TestUpdateTask_RescheduleAndPayload(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := New(pool)

	id := makeTask(t, s, ctx, KindReminder, "active")
	spec, err := ParseSchedule("every", "", 30, time.Time{}, "Europe/Rome")
	if err != nil {
		t.Fatalf("ParseSchedule: %v", err)
	}
	next, err := FirstFire(spec, time.Now())
	if err != nil {
		t.Fatalf("FirstFire: %v", err)
	}
	if err := s.UpdateTask(ctx, id, UpdateTaskParams{
		Spec: spec, NextRunAt: next, Payload: []byte(`{"text":"edited"}`), NotifyRoute: "whatsapp",
	}); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	got, err := s.GetTask(ctx, id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.ScheduleKind != KindEvery || got.EveryMinutes != 30 {
		t.Errorf("reschedule not applied: kind=%q every=%d", got.ScheduleKind, got.EveryMinutes)
	}
	if got.CronExpr != "" {
		t.Errorf("cron expr must clear on switch to every, got %q", got.CronExpr)
	}
	// payload is a jsonb column: Postgres reserializes it to canonical form (a space after
	// the colon), so a byte-for-byte compare against the literal we wrote spuriously fails.
	// Compare the JSON semantically by re-marshalling to compact form instead.
	var gotPayload map[string]any
	if err := json.Unmarshal(got.Payload, &gotPayload); err != nil {
		t.Fatalf("payload is not valid JSON: %v (raw %s)", err, got.Payload)
	}
	compactPayload, _ := json.Marshal(gotPayload)
	if got.NotifyRoute != "whatsapp" || string(compactPayload) != `{"text":"edited"}` {
		t.Errorf("payload/notify not updated: notify=%q payload=%s", got.NotifyRoute, got.Payload)
	}
}
