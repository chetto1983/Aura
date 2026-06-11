//go:build db_integration

// Integration tests for the dispatch seam against a live Postgres: the dispatcher
// writes the handler summary into agent_job_runs and the completion is idempotent on
// the completed_with_hash key (SC#2). Run via:
//
//	go test -tags db_integration -race -run TestDispatch ./internal/cron -count=1
package cron

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDispatchWritesSummaryToRun(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s := New(pool)

	spec, _ := ParseSchedule("every", "", 5, time.Time{}, "Europe/Rome")
	task, err := s.CreateTask(ctx, CreateTaskParams{
		Kind: KindReminder, Spec: spec, Payload: []byte(`{"text":"hydrate"}`),
		NextRunAt: time.Now().Add(5 * time.Minute), NotifyRoute: "stdout",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() { cleanupTask(t, pool, task.ID) })

	run, err := s.InsertRun(ctx, task.ID, 0)
	if err != nil {
		t.Fatalf("InsertRun: %v", err)
	}

	h := &fakeHandler{meta: HandlerMeta{Kind: KindReminder}, summary: "hydrate"}
	var buf stdoutBuf
	d := NewDispatch(map[TaskKind]Handler{KindReminder: h}, DispatchDeps{
		Store: s, Notifier: &compositeNotifier{out: &buf},
	})

	domainTask, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if err := d.Dispatch(ctx, domainTask, &Claim{RunID: run.ID}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	got, err := s.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Status != "completed" || got.Summary != "hydrate" {
		t.Fatalf("run not completed with summary: status=%q summary=%q", got.Status, got.Summary)
	}
	if got.CompletedWithHash != run.ID {
		t.Fatalf("completed_with_hash = %q, want the run id %q", got.CompletedWithHash, run.ID)
	}
}

func TestDispatchCompletionIsIdempotent(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s := New(pool)

	spec, _ := ParseSchedule("every", "", 5, time.Time{}, "Europe/Rome")
	task, err := s.CreateTask(ctx, CreateTaskParams{
		Kind: KindReminder, Spec: spec, NextRunAt: time.Now().Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() { cleanupTask(t, pool, task.ID) })
	run, err := s.InsertRun(ctx, task.ID, 0)
	if err != nil {
		t.Fatalf("InsertRun: %v", err)
	}

	h := &fakeHandler{meta: HandlerMeta{Kind: KindReminder}, summary: "once"}
	var buf stdoutBuf
	d := NewDispatch(map[TaskKind]Handler{KindReminder: h}, DispatchDeps{Store: s, Notifier: &compositeNotifier{out: &buf}})
	domainTask, _ := s.GetTask(ctx, task.ID)

	// A redelivered dispatch of the SAME run hits the completed_with_hash UNIQUE
	// constraint on the second CompleteRun — swallowed as ErrAlreadyRunning by the
	// store, logged by the dispatcher, never a crash (SC#2).
	if err := d.Dispatch(ctx, domainTask, &Claim{RunID: run.ID}); err != nil {
		t.Fatalf("Dispatch #1: %v", err)
	}
	if err := d.Dispatch(ctx, domainTask, &Claim{RunID: run.ID}); err != nil {
		t.Fatalf("Dispatch #2 (redelivery) must not error: %v", err)
	}

	got, err := s.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Summary != "once" {
		t.Fatalf("redelivery must not overwrite the summary, got %q", got.Summary)
	}
}

func TestPendingNotificationQuietHoursDispatchAndSweep(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s := New(pool)

	spec, _ := ParseSchedule("every", "", 5, time.Time{}, "Europe/Rome")
	task, err := s.CreateTask(ctx, CreateTaskParams{
		Kind: KindAgentJob, Spec: spec, Payload: []byte(`{"goal":"summarize the news"}`),
		NextRunAt: time.Now().Add(5 * time.Minute), NotifyRoute: "whatsapp",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() { cleanupTask(t, pool, task.ID) })

	run, err := s.InsertRun(ctx, task.ID, 0)
	if err != nil {
		t.Fatalf("InsertRun: %v", err)
	}
	// Postgres timestamptz persists at microsecond precision.
	notifyAfter := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	notif := &captureNotifier{}
	h := &fakeHandler{meta: HandlerMeta{Kind: KindAgentJob}, summary: "routine summary"}
	d := NewDispatch(map[TaskKind]Handler{KindAgentJob: h}, DispatchDeps{
		Store: s, Notifier: notif,
		QuietHours: func(string) bool { return true },
		QuietHoursEnd: func(string) (time.Time, bool) {
			return notifyAfter, true
		},
	})
	domainTask, _ := s.GetTask(ctx, task.ID)
	if err := d.Dispatch(ctx, domainTask, &Claim{RunID: run.ID}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(notif.texts) != 0 {
		t.Fatalf("quiet-hours dispatch must not notify immediately, got %v", notif.texts)
	}

	var notificationID, status string
	var persistedAfter time.Time
	if err := pool.QueryRow(ctx, `
		SELECT id::text, status, notify_after
		FROM aura.pending_notifications
		WHERE run_id = $1`, run.ID).Scan(&notificationID, &status, &persistedAfter); err != nil {
		t.Fatalf("read pending notification: %v", err)
	}
	if status != "pending" || notificationID == "" {
		t.Fatalf("pending row mismatch: id=%q status=%q", notificationID, status)
	}
	if !persistedAfter.Equal(notifyAfter) {
		t.Fatalf("notify_after = %s, want %s", persistedAfter, notifyAfter)
	}

	if _, err := pool.Exec(ctx, `UPDATE aura.pending_notifications SET notify_after = now() - interval '1 second' WHERE id = $1`, notificationID); err != nil {
		t.Fatalf("make pending notification due: %v", err)
	}
	if err := d.sweepNotifications(ctx); err != nil {
		t.Fatalf("sweepNotifications: %v", err)
	}
	if len(notif.texts) != 1 || notif.texts[0] != "routine summary" {
		t.Fatalf("sweep must deliver the deferred notification, got %v", notif.texts)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM aura.pending_notifications WHERE id = $1`, notificationID).Scan(&status); err != nil {
		t.Fatalf("read delivered notification: %v", err)
	}
	if status != "delivered" {
		t.Fatalf("status after sweep = %q, want delivered", status)
	}
}

func TestPendingNotificationFailedSelfSendBoundedRetry(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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

	run, err := s.InsertRun(ctx, task.ID, 0)
	if err != nil {
		t.Fatalf("InsertRun: %v", err)
	}
	notif := &captureNotifier{errs: []error{
		errors.New("mcp down"),
		errors.New("still down 1"),
		errors.New("still down 2"),
		errors.New("still down 3"),
		errors.New("should not be called"),
	}}
	h := &fakeHandler{meta: HandlerMeta{Kind: KindAgentJob}, summary: "routine summary"}
	d := NewDispatch(map[TaskKind]Handler{KindAgentJob: h}, DispatchDeps{Store: s, Notifier: notif})
	domainTask, _ := s.GetTask(ctx, task.ID)
	if err := d.Dispatch(ctx, domainTask, &Claim{RunID: run.ID}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	var notificationID, status string
	var attempts int
	if err := pool.QueryRow(ctx, `
		SELECT id::text, status, attempts
		FROM aura.pending_notifications
		WHERE run_id = $1`, run.ID).Scan(&notificationID, &status, &attempts); err != nil {
		t.Fatalf("read failed notification: %v", err)
	}
	if status != "failed" || attempts != 0 {
		t.Fatalf("after failed send status=%q attempts=%d, want failed/0", status, attempts)
	}

	for i := 0; i < pendingNotificationAttemptBound; i++ {
		if err := d.sweepNotifications(ctx); err != nil {
			t.Fatalf("sweepNotifications #%d: %v", i+1, err)
		}
	}
	if err := pool.QueryRow(ctx, `SELECT status, attempts FROM aura.pending_notifications WHERE id = $1`, notificationID).Scan(&status, &attempts); err != nil {
		t.Fatalf("read bounded notification: %v", err)
	}
	if status != "failed" || attempts != pendingNotificationAttemptBound {
		t.Fatalf("after bounded retries status=%q attempts=%d, want failed/%d", status, attempts, pendingNotificationAttemptBound)
	}

	if err := d.sweepNotifications(ctx); err != nil {
		t.Fatalf("sweepNotifications past bound: %v", err)
	}
	if len(notif.texts) != 1+pendingNotificationAttemptBound {
		t.Fatalf("notification retried past bound: calls=%d want %d", len(notif.texts), 1+pendingNotificationAttemptBound)
	}
}

// TestDispatchPendingNotificationIdentityRoundTrip pins the 0014 schema work: the
// identity_id snapshot threaded by InsertPendingNotification round-trips through
// SweepDueNotifications + the PendingNotification projection (the Step-2 route-back
// key). It then asserts migration 0014 reverts cleanly (down drops identity_id) and
// re-up re-adds it — the no-skip-as-green reversibility gate (T-20-13). This test runs
// against a live Postgres; a sub-second runtime is a skip tell.
func TestDispatchPendingNotificationIdentityRoundTrip(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s := New(pool)

	spec, _ := ParseSchedule("every", "", 5, time.Time{}, "Europe/Rome")
	task, err := s.CreateTask(ctx, CreateTaskParams{
		Kind: KindAgentJob, Spec: spec, Payload: []byte(`{"goal":"summarize the news"}`),
		NextRunAt: time.Now().Add(5 * time.Minute), NotifyRoute: "whatsapp",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() { cleanupTask(t, pool, task.ID) })
	run, err := s.InsertRun(ctx, task.ID, 0)
	if err != nil {
		t.Fatalf("InsertRun: %v", err)
	}

	const wantIdentity = "id-roundtrip"
	// Insert a DUE pending row carrying identity_id (notify_after in the past so the
	// sweep selects it). Status pending so it is swept on the first pass.
	inserted, err := s.InsertPendingNotification(ctx, InsertPendingNotificationParams{
		RunID:       run.ID,
		NotifyRoute: "whatsapp",
		Body:        "round-trip body",
		NotifyAfter: time.Now().UTC().Add(-time.Second),
		Status:      "pending",
		IdentityID:  wantIdentity,
	})
	if err != nil {
		t.Fatalf("InsertPendingNotification: %v", err)
	}
	if inserted.IdentityID != wantIdentity {
		t.Fatalf("InsertPendingNotification returned identity_id %q, want %q", inserted.IdentityID, wantIdentity)
	}

	rows, err := s.SweepDueNotifications(ctx, pendingNotificationAttemptBound, pendingNotificationSweepLimit)
	if err != nil {
		t.Fatalf("SweepDueNotifications: %v", err)
	}
	var got *PendingNotification
	for i := range rows {
		if rows[i].ID == inserted.ID {
			got = &rows[i]
			break
		}
	}
	if got == nil {
		t.Fatalf("swept rows did not include the inserted notification %q (got %d rows)", inserted.ID, len(rows))
	}
	if got.IdentityID != wantIdentity {
		t.Fatalf("identity_id did not round-trip through SweepDueNotifications: got %q, want %q", got.IdentityID, wantIdentity)
	}

	// Reversibility: down -1 drops identity_id, re-up re-adds it. Probe the column via
	// information_schema so we assert the SCHEMA, not just a query error.
	migrateURL := os.Getenv("AURA_DB_MIGRATE_URL")
	if migrateURL == "" {
		t.Fatal("AURA_DB_MIGRATE_URL unset after migratedPool — reversibility step needs the migrate DSN")
	}
	if columnExists(ctx, t, pool, "identity_id") != true {
		t.Fatal("identity_id column missing before the down step (migratedPool should have applied 0014)")
	}
	if err := db.MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("MigrateSteps down -1 (revert 0014): %v", err)
	}
	if columnExists(ctx, t, pool, "identity_id") != false {
		t.Fatal("identity_id column still present after the 0014 down migration")
	}
	if err := db.MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("MigrateSteps up 1 (re-apply 0014): %v", err)
	}
	if columnExists(ctx, t, pool, "identity_id") != true {
		t.Fatal("identity_id column missing after re-applying 0014 — re-up is not clean")
	}
}

// columnExists probes information_schema for aura.pending_notifications.<col>.
func columnExists(ctx context.Context, t *testing.T, pool *pgxpool.Pool, col string) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'aura'
			  AND table_name = 'pending_notifications'
			  AND column_name = $1)`, col).Scan(&exists); err != nil {
		t.Fatalf("probe column %q: %v", col, err)
	}
	return exists
}

// TestDispatchCompletesRunOnCancelledRootCtx is the M-h regression: a shutdown mid-run
// cancels the dispatch root ctx, but the terminal run-state write must still land (on
// the detached ctx) so the run is NOT stuck 'running' until the 90s orphan window.
// BEFORE the fix CompleteRun(ctx,…) ran on the cancelled root ctx, pgx rejected it, and
// the dispatcher only logged — leaving the run 'running'. AFTER, the detached
// WithoutCancel+deadline ctx writes the terminal state.
func TestDispatchCompletesRunOnCancelledRootCtx(t *testing.T) {
	pool := migratedPool(t)
	setupCtx, setupCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer setupCancel()
	s := New(pool)

	spec, _ := ParseSchedule("every", "", 5, time.Time{}, "Europe/Rome")
	task, err := s.CreateTask(setupCtx, CreateTaskParams{
		Kind: KindReminder, Spec: spec, Payload: []byte(`{"text":"hydrate"}`),
		NextRunAt: time.Now().Add(5 * time.Minute), NotifyRoute: "stdout",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() { cleanupTask(t, pool, task.ID) })

	run, err := s.InsertRun(setupCtx, task.ID, 0)
	if err != nil {
		t.Fatalf("InsertRun: %v", err)
	}

	h := &fakeHandler{meta: HandlerMeta{Kind: KindReminder}, summary: "hydrate"}
	var buf stdoutBuf
	d := NewDispatch(map[TaskKind]Handler{KindReminder: h}, DispatchDeps{
		Store: s, Notifier: &compositeNotifier{out: &buf},
	})
	domainTask, err := s.GetTask(setupCtx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}

	// Simulate a shutdown signal mid-run: the dispatch root ctx is already cancelled
	// when complete() runs the terminal write.
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := d.Dispatch(cancelledCtx, domainTask, &Claim{RunID: run.ID}); err != nil {
		t.Fatalf("Dispatch on cancelled ctx: %v", err)
	}

	// The terminal write must have landed on the detached ctx — read on a FRESH ctx.
	readCtx, readCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer readCancel()
	got, err := s.GetRun(readCtx, run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Status == "running" {
		t.Fatalf("run still 'running' after a cancelled-root dispatch — terminal write was dropped (M-h)")
	}
	if got.Status != "completed" || got.Summary != "hydrate" {
		t.Fatalf("run not completed with summary on cancelled root ctx: status=%q summary=%q", got.Status, got.Summary)
	}
}

// stdoutBuf is a tiny io.Writer that discards notification output in the integration
// tests (the delivery path is unit-tested in notify_test.go).
type stdoutBuf struct{}

func (stdoutBuf) Write(p []byte) (int, error) { return len(p), nil }
