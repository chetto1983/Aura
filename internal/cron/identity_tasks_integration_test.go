//go:build db_integration

// Integration tests for what de-provisioning does to an identity's tasks (pause.go,
// store_manage.go) against a real Postgres, as aura_app: deactivation pauses them, the purge
// removes them, and the cascade takes their runs and notifications.
package cron

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
)

func newOwner() string { return uuid.Must(uuid.NewV7()).String() }

// ownedTask is a past-due task of owner, so DueTasks would return it were it active.
func ownedTask(t *testing.T, s *Store, ctx context.Context, owner string, kind TaskKind, status string) string {
	t.Helper()
	id := makeTask(t, s, ctx, kind, status)
	if _, err := s.pool.Exec(ctx, `
		UPDATE aura.scheduler_tasks SET identity_id = $2, next_run_at = now() - interval '1 minute'
		WHERE id = $1`, id, owner); err != nil {
		t.Fatalf("own task: %v", err)
	}
	return id
}

func dueIDs(t *testing.T, s *Store, ctx context.Context) map[string]bool {
	t.Helper()
	due, err := s.DueTasks(ctx, 1000)
	if err != nil {
		t.Fatalf("DueTasks: %v", err)
	}
	ids := make(map[string]bool, len(due))
	for _, task := range due {
		ids[task.ID] = true
	}
	return ids
}

func TestDeactivationLeavesTheDeploymentsTasksRunning(t *testing.T) {
	s := New(migratedPool(t))
	ctx := pauseCtx(t)
	owner := newOwner()
	kept := make(map[TaskKind]string, len(systemKinds))
	for _, kind := range systemKinds {
		kept[kind] = ownedTask(t, s, ctx, owner, kind, "active")
	}
	watch := ownedTask(t, s, ctx, owner, KindPIMDigest, "active")

	if err := s.PauseIdentityTasks(ctx, owner); err != nil {
		t.Fatalf("PauseIdentityTasks: %v", err)
	}

	for kind, id := range kept {
		got, err := s.GetTask(ctx, id)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if got.Status != "active" || got.PausedReason != "" {
			t.Errorf("%s = %s/%q, want it still active", kind, got.Status, got.PausedReason)
		}
	}
	if got, err := s.GetTask(ctx, watch); err != nil || got.Status != "paused" {
		t.Fatalf("the person's digest = %+v, %v", got, err)
	}
}

func TestDeactivationPausesOnlyTheIdentitysActiveTasks(t *testing.T) {
	s := New(migratedPool(t))
	ctx := pauseCtx(t)
	owner := newOwner()

	active := ownedTask(t, s, ctx, owner, KindAgentJob, "active")
	watch := ownedTask(t, s, ctx, owner, KindReminder, "active")
	sweep := ownedTask(t, s, ctx, owner, KindIdentityPurge, "active")
	awaitingApproval := ownedTask(t, s, ctx, owner, KindAgentJob, "pending_approval")
	pausedByHand := ownedTask(t, s, ctx, owner, KindReminder, "active")
	if err := s.PauseTask(ctx, pausedByHand); err != nil {
		t.Fatal(err)
	}
	cancelled := ownedTask(t, s, ctx, owner, KindReminder, "active")
	if err := s.CancelTask(ctx, cancelled); err != nil {
		t.Fatal(err)
	}
	backup := ownedTask(t, s, ctx, owner, KindBackupPostgres, "active")
	someoneElses := ownedTask(t, s, ctx, newOwner(), KindAgentJob, "active")

	if err := s.PauseIdentityTasks(ctx, owner); err != nil {
		t.Fatalf("PauseIdentityTasks: %v", err)
	}

	for _, tc := range []struct {
		name, id, status, reason string
	}{
		{"active agent job", active, "paused", PausedByIdentityDeactivated},
		{"active reminder", watch, "paused", PausedByIdentityDeactivated},
		{"awaiting approval", awaitingApproval, "pending_approval", ""},
		{"paused by hand", pausedByHand, "paused", PausedByOperator},
		{"cancelled", cancelled, "cancelled", ""},
		{"backup", backup, "active", ""},
		{"system sweep", sweep, "active", ""},
		{"another identity's", someoneElses, "active", ""},
	} {
		got, err := s.GetTask(ctx, tc.id)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got.Status != tc.status || got.PausedReason != tc.reason {
			t.Errorf("%s = %s/%q, want %s/%q", tc.name, got.Status, got.PausedReason, tc.status, tc.reason)
		}
	}

	due := dueIDs(t, s, ctx)
	if due[active] || due[watch] {
		t.Fatal("a task paused by deactivation is still due")
	}
	if !due[someoneElses] {
		t.Fatal("another identity's task stopped being due")
	}
	if !due[sweep] {
		t.Fatal("the deployment's sweep stopped with its owner")
	}
}

func TestDeactivatingTwiceChangesNothing(t *testing.T) {
	s := New(migratedPool(t))
	ctx := pauseCtx(t)
	owner := newOwner()
	id := ownedTask(t, s, ctx, owner, KindAgentJob, "active")

	if err := s.PauseIdentityTasks(ctx, owner); err != nil {
		t.Fatalf("first: %v", err)
	}
	first, err := s.GetTask(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PauseIdentityTasks(ctx, owner); err != nil {
		t.Fatalf("second: %v", err)
	}
	second, err := s.GetTask(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if !second.UpdatedAt.Equal(first.UpdatedAt) || second.Status != first.Status || second.PausedReason != first.PausedReason {
		t.Fatalf("second call changed the row: %+v, then %+v", first, second)
	}
}

// The run finishing after the pause is the one a deactivation does not stop. It must not lift
// the pause or replace its reason.
func TestARunEndingAfterDeactivationLeavesThePause(t *testing.T) {
	s := New(migratedPool(t))
	ctx := pauseCtx(t)
	owner := newOwner()
	id := ownedTask(t, s, ctx, owner, KindAgentJob, "active")
	if err := s.PauseIdentityTasks(ctx, owner); err != nil {
		t.Fatal(err)
	}

	if _, err := s.RecordRunOutcome(ctx, id, false, 1); err != nil {
		t.Fatalf("RecordRunOutcome: %v", err)
	}
	got, err := s.GetTask(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "paused" || got.PausedReason != PausedByIdentityDeactivated {
		t.Fatalf("after the run = %s/%q", got.Status, got.PausedReason)
	}
}

// seedRunAndNotification gives a task one run and one undelivered notification whose body would
// be what the person was told.
func seedRunAndNotification(t *testing.T, s *Store, ctx context.Context, taskID, owner string) (runID, notificationID string) {
	t.Helper()
	runID, notificationID = newOwner(), newOwner()
	if _, err := s.pool.Exec(ctx, `INSERT INTO aura.agent_job_runs (id, task_id, status) VALUES ($1, $2, 'failed')`, runID, taskID); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO aura.pending_notifications (id, run_id, notify_route, body, notify_after, status, identity_id)
		VALUES ($1, $2, 'stdout', 'Mario Rossi: fattura scaduta', now(), 'pending', $3)`, notificationID, runID, owner); err != nil {
		t.Fatalf("seed notification: %v", err)
	}
	return runID, notificationID
}

func rowExists(t *testing.T, s *Store, ctx context.Context, table, id string) bool {
	t.Helper()
	var found bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM aura.`+table+` WHERE id = $1)`, id).Scan(&found); err != nil {
		t.Fatalf("read %s: %v", table, err)
	}
	return found
}

func TestPurgeRemovesAnIdentitysTasksRunsAndNotifications(t *testing.T) {
	s := New(migratedPool(t))
	ctx := pauseCtx(t)
	owner, other := newOwner(), newOwner()

	paused := ownedTask(t, s, ctx, owner, KindAgentJob, "active")
	if err := s.PauseIdentityTasks(ctx, owner); err != nil {
		t.Fatal(err)
	}
	awaitingApproval := ownedTask(t, s, ctx, owner, KindAgentJob, "pending_approval")
	run, notification := seedRunAndNotification(t, s, ctx, paused, owner)

	kept := ownedTask(t, s, ctx, other, KindAgentJob, "active")
	keptRun, keptNotification := seedRunAndNotification(t, s, ctx, kept, other)
	deployment := make(map[TaskKind]string, len(systemKinds))
	for _, kind := range systemKinds {
		deployment[kind] = ownedTask(t, s, ctx, owner, kind, "active")
	}

	if err := s.DeleteIdentityTasks(ctx, owner); err != nil {
		t.Fatalf("DeleteIdentityTasks: %v", err)
	}

	for kind, id := range deployment {
		got, err := s.GetTask(ctx, id)
		if err != nil {
			t.Fatalf("the purge took %s: %v", kind, err)
		}
		if got.Status != "active" {
			t.Errorf("%s = %s, want it still active", kind, got.Status)
		}
	}

	for _, row := range []struct{ table, id string }{
		{"scheduler_tasks", paused}, {"scheduler_tasks", awaitingApproval},
		{"agent_job_runs", run}, {"pending_notifications", notification},
	} {
		if rowExists(t, s, ctx, row.table, row.id) {
			t.Errorf("%s %s survived the purge", row.table, row.id)
		}
	}
	for _, row := range []struct{ table, id string }{
		{"scheduler_tasks", kept}, {"agent_job_runs", keptRun}, {"pending_notifications", keptNotification},
	} {
		if !rowExists(t, s, ctx, row.table, row.id) {
			t.Errorf("another identity's %s %s went with the purge", row.table, row.id)
		}
	}

	if err := s.DeleteIdentityTasks(ctx, owner); err != nil {
		t.Fatalf("second purge: %v", err)
	}
}

// sweepOfAPurgedOwner is a system handler that says who it ran as.
type sweepOfAPurgedOwner struct{ ranAs string }

func (h *sweepOfAPurgedOwner) Meta() HandlerMeta {
	return HandlerMeta{Kind: KindRetentionSweep, MaxDuration: time.Minute}
}

func (h *sweepOfAPurgedOwner) Run(ctx context.Context, _ Job) (string, error) {
	h.ranAs = identityctx.IdentityID(ctx)
	return "swept", nil
}

// Enrolment hands the deployment's sweeps to the operator's identity, so purging that identity
// leaves them owned by a row that no longer exists. One of them is the purge sweep itself. They
// must go on firing: claimed, dispatched and completed through the real scheduler.
func TestASystemTaskOfAPurgedIdentityIsStillClaimedAndDispatched(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)
	ctx := pauseCtx(t)
	owner := newOwner()
	if _, err := pool.Exec(ctx, `INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')`, owner, "purged-"+owner); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id = $1`, owner) })
	sweep := ownedTask(t, s, ctx, owner, KindRetentionSweep, "active")

	if err := s.DeleteIdentityTasks(ctx, owner); err != nil {
		t.Fatalf("DeleteIdentityTasks: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM aura.identities WHERE id = $1`, owner); err != nil {
		t.Fatalf("delete identity: %v", err)
	}

	due, err := s.DueTasks(ctx, 1000)
	if err != nil {
		t.Fatalf("DueTasks: %v", err)
	}
	found := slices.IndexFunc(due, func(task Task) bool { return task.ID == sweep })
	if found < 0 {
		t.Fatal("the sweep of a purged identity is not due")
	}
	task := due[found]

	scheduler := NewScheduler(pool, s, SchedulerConfig{MaxConcurrent: 1, TickInterval: time.Second})
	claim, err := scheduler.claim(ctx, task)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	defer claim.release(ctx)

	handler := &sweepOfAPurgedOwner{}
	dispatch := NewDispatch(map[TaskKind]Handler{KindRetentionSweep: handler}, DispatchDeps{Store: s})
	if err := dispatch.Dispatch(ctx, task, claim); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if handler.ranAs != owner {
		t.Fatalf("the sweep ran as %q", handler.ranAs)
	}
	run, err := s.GetRun(ctx, claim.RunID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.Status != "completed" {
		t.Fatalf("run = %+v", run)
	}
	if got, err := s.GetTask(ctx, sweep); err != nil || got.Status != "active" {
		t.Fatalf("after the run = %+v, %v", got, err)
	}
}

// The down migration runs while tasks can be paused this way, so it has to keep them paused and
// still land the narrower CHECK.
func TestMigration0150DownKeepsADeactivatedTaskPaused(t *testing.T) {
	migrateURL := disposableMigrateURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	positionDisposableMigration(t, ctx, migrateURL, 150)

	pool, err := db.Open(ctx, &db.Config{URL: migrateURL})
	if err != nil {
		t.Fatalf("open disposable database: %v", err)
	}
	defer pool.Close()

	id := newOwner()
	if _, err := pool.Exec(ctx, `
		INSERT INTO aura.scheduler_tasks
			(id, kind, schedule_kind, every_minutes, tz, payload, status, next_run_at, notify_route, identity_id, paused_reason)
		VALUES ($1, 'reminder', 'every', 5, 'UTC', '{}'::jsonb, 'paused', now(), 'none', $2, 'identity_deactivated')`, id, newOwner()); err != nil {
		t.Fatalf("seed a deactivated pause: %v", err)
	}

	if err := db.MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("revert 0150: %v", err)
	}
	var status, reason string
	if err := pool.QueryRow(ctx, `SELECT status, paused_reason FROM aura.scheduler_tasks WHERE id = $1`, id).Scan(&status, &reason); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if status != "paused" || reason != PausedByOperator {
		t.Fatalf("after the down = %s/%s", status, reason)
	}
	if _, err := pool.Exec(ctx, `UPDATE aura.scheduler_tasks SET paused_reason = 'identity_deactivated' WHERE id = $1`, id); err == nil {
		t.Fatal("the down left the wider paused_reason CHECK")
	}

	if err := db.MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("re-apply 0150: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE aura.scheduler_tasks SET paused_reason = 'identity_deactivated' WHERE id = $1`, id); err != nil {
		t.Fatalf("re-applied 0150 refuses the reason: %v", err)
	}
}
