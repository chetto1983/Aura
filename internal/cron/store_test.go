//go:build db_integration

// Integration tests for internal/cron.Store. Requires a running Postgres with the
// Phase-10 migrations applied (0009 creates scheduler_tasks + agent_job_runs):
//
//	make db-up && aura db migrate           # or the WSL equivalent
//	AURA_DB_URL + AURA_DB_MIGRATE_URL + POSTGRES_PASSWORD set in env
//
// Run via:
//
//	go test -tags db_integration -race ./internal/cron -count=1
//
// No-skip-as-green: envOrSkip t.Fatals under $CI when the DSN is unset, so a
// skipped tier can never pass as green in the pipeline.
package cron

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/dbtest"
	"github.com/jackc/pgx/v5/pgxpool"
)

// envOrSkip mirrors internal/identity/store_test.go: skip locally, fail-loud under
// CI so a missing DSN never reports a falsely-green integration job.
func envOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("integration test requires %s, but it is unset under CI — "+
				"a skipped integration test must not pass as green; wire it in ci.yml", key)
		}
		t.Skipf("integration test requires %s; set it and re-run (e.g. via .env + make db-up)", key)
	}
	return v
}

// pgDSN builds a connection string for one role against one database on the test
// cluster. PGHOST/PGPORT default to the compose stack.
func pgDSN(role, password, database string) string {
	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", role, password, host, port, database)
}

// migratedPool ensures roles + migrations are applied, then returns an aura_app
// pool ready for Store use. Closed via t.Cleanup.
func migratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := dbtest.MigrateURL(t, envOrSkip(t, "AURA_DB_MIGRATE_URL"))
	appURL := envOrSkip(t, "AURA_DB_URL")

	bootstrap := pgDSN("aura", pwd, "aura")

	if err := db.EnsureRoles(ctx, bootstrap, pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	if _, err := db.Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := db.Open(ctx, &db.Config{URL: appURL})
	if err != nil {
		t.Fatalf("Open (aura_app): %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// disposableMigrateURL creates an EMPTY database owned by aura_migrate, returns its
// migrate DSN, and drops it when the test ends.
//
// A migration-reversibility probe must never walk the SHARED integration database.
// Stepping it down deletes every other package's fixtures, and a single failing down
// step leaves the schema torn down for the whole rest of the tier: measured 2026-08-03
// at ~176 failures across 13 packages, all of them downstream noise from one bad
// statement. Worse, walking far enough back crosses migration 0086 — a declared
// one-way door that drops the adaptive plane and says in its own comment that stepping
// past it will fail. Reversibility is a property of ONE migration; prove it on three
// steps of a throwaway database, not on a hundred-and-fifty against the live one.
func disposableMigrateURL(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	admin, err := db.Open(ctx, &db.Config{URL: pgDSN("aura", pwd, "postgres")})
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	defer admin.Close()

	name := fmt.Sprintf("aura_cron_rev_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, `CREATE DATABASE "`+name+`" OWNER aura_migrate`); err != nil {
		t.Fatalf("create disposable database %s: %v", name, err)
	}
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer dropCancel()
		dropPool, err := db.Open(dropCtx, &db.Config{URL: pgDSN("aura", pwd, "postgres")})
		if err != nil {
			t.Logf("drop %s: reopen admin pool: %v", name, err)
			return
		}
		defer dropPool.Close()
		if _, err := dropPool.Exec(dropCtx, `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`); err != nil {
			t.Logf("drop %s: %v", name, err)
		}
	})

	// EnsureRoles is per-DATABASE for the CREATE grant 0001_init needs, so it must run
	// against the new database, not the shared one.
	if err := db.EnsureRoles(ctx, pgDSN("aura", pwd, name), pwd); err != nil {
		t.Fatalf("ensure roles on %s: %v", name, err)
	}
	return pgDSN("aura_migrate", pwd, name)
}

// cleanupTask removes a task (and its run rows cascade) so re-runs start clean.
func cleanupTask(t *testing.T, pool *pgxpool.Pool, id string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		"DELETE FROM aura.scheduler_tasks WHERE id = $1", id); err != nil {
		t.Logf("cleanup task %q (best-effort): %v", id, err)
	}
}

func TestCreateGetTask_RoundTrip(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := New(pool)

	spec, err := ParseSchedule("cron", "30 9 * * *", 0, time.Time{}, "Europe/Rome")
	if err != nil {
		t.Fatalf("ParseSchedule: %v", err)
	}
	next, err := NextRunAt(spec, time.Now())
	if err != nil {
		t.Fatalf("NextRunAt: %v", err)
	}

	created, err := s.CreateTask(ctx, CreateTaskParams{
		Kind:        KindAgentJob,
		Spec:        spec,
		Payload:     []byte(`{"goal":"morning summary"}`),
		StepBudget:  12,
		NextRunAt:   next,
		NotifyRoute: "stdout",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() { cleanupTask(t, pool, created.ID) })

	got, err := s.GetTask(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Kind != KindAgentJob || got.ScheduleKind != KindCron || got.CronExpr != "30 9 * * *" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if got.TZ != "Europe/Rome" || got.StepBudget != 12 || got.NotifyRoute != "stdout" {
		t.Errorf("round-trip field mismatch: tz=%q budget=%d notify=%q", got.TZ, got.StepBudget, got.NotifyRoute)
	}
	if got.Status != "active" {
		t.Errorf("new task status = %q, want active", got.Status)
	}
}

// TestCreateTask_GatedStatusIsAtomic is the WR-03 regression: a task scoring routed
// to pending_approval is persisted with that status in the SINGLE CreateTask INSERT,
// never as active-then-UPDATE. A pending_approval row is also NOT due — DueTasks
// (status='active') can never select it, so the destructive gate holds even before
// any tick (T-10-12 / D-27).
func TestCreateTask_GatedStatusIsAtomic(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := New(pool)

	spec, _ := ParseSchedule("every", "", 5, time.Time{}, "Europe/Rome")
	created, err := s.CreateTask(ctx, CreateTaskParams{
		Kind:      KindAgentJob,
		Spec:      spec,
		Payload:   []byte(`{"goal":"drop the staging database"}`),
		NextRunAt: time.Now().Add(-time.Minute), // would be due IF it were active
		Status:    "pending_approval",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() { cleanupTask(t, pool, created.ID) })

	if created.Status != "pending_approval" {
		t.Fatalf("created status = %q, want pending_approval (atomic gate)", created.Status)
	}
	got, err := s.GetTask(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != "pending_approval" {
		t.Errorf("persisted status = %q, want pending_approval", got.Status)
	}

	// The gated row is not selectable by the tick even though next_run_at is past.
	due, err := s.DueTasks(ctx, 10)
	if err != nil {
		t.Fatalf("DueTasks: %v", err)
	}
	for _, d := range due {
		if d.ID == created.ID {
			t.Errorf("pending_approval task wrongly selected as due: %s", d.ID)
		}
	}
}

func TestGetTask_Missing(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := New(pool)

	_, err := s.GetTask(ctx, "00000000-0000-0000-0000-0000000000ff")
	if !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("GetTask(absent) = %v, want ErrTaskNotFound", err)
	}
}

func TestCompleteRun_IdempotencyHash(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := New(pool)

	spec, _ := ParseSchedule("every", "", 5, time.Time{}, "Europe/Rome")
	created, err := s.CreateTask(ctx, CreateTaskParams{
		Kind: KindReminder, Spec: spec, NextRunAt: time.Now().Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() { cleanupTask(t, pool, created.ID) })

	run1, err := s.InsertRun(ctx, created.ID, 0)
	if err != nil {
		t.Fatalf("InsertRun #1: %v", err)
	}
	run2, err := s.InsertRun(ctx, created.ID, 0)
	if err != nil {
		t.Fatalf("InsertRun #2: %v", err)
	}

	const hash = "dedupe-hash-abc"
	if err := s.CompleteRun(ctx, CompleteRunParams{RunID: run1.ID, Status: "completed", Summary: "done", CompletedHash: hash}); err != nil {
		t.Fatalf("CompleteRun #1: %v", err)
	}
	// A second completion reusing the same hash trips the UNIQUE constraint and is
	// swallowed as ErrAlreadyRunning (SC#2 at-least-once idempotency).
	err = s.CompleteRun(ctx, CompleteRunParams{RunID: run2.ID, Status: "completed", Summary: "dup", CompletedHash: hash})
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("duplicate completion hash = %v, want ErrAlreadyRunning", err)
	}
}

func TestCreateRunAndAdvance_Atomic(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := New(pool)

	spec, _ := ParseSchedule("every", "", 5, time.Time{}, "Europe/Rome")
	first := time.Now().Add(5 * time.Minute).Truncate(time.Second)
	created, err := s.CreateTask(ctx, CreateTaskParams{Kind: KindReminder, Spec: spec, NextRunAt: first})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() { cleanupTask(t, pool, created.ID) })

	advanced := first.Add(5 * time.Minute)
	run, err := s.CreateRunAndAdvance(ctx, created.ID, 0, advanced)
	if err != nil {
		t.Fatalf("CreateRunAndAdvance: %v", err)
	}
	if run.Status != "running" || run.TaskID != created.ID {
		t.Errorf("opened run = %+v, want running for task %s", run, created.ID)
	}
	got, err := s.GetTask(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if !got.NextRunAt.Equal(advanced.UTC()) {
		t.Errorf("next_run_at after advance = %s, want %s", got.NextRunAt, advanced.UTC())
	}
}

// TestDueTasks_ClampsBadLimit is the WR-02 regression: a non-positive limit must not
// silently dispatch nothing (LIMIT 0) and a huge limit must not wrap to a negative
// LIMIT (a Postgres error). Both are floored to 1 at the store boundary, so a single
// overdue task is still selectable.
func TestDueTasks_ClampsBadLimit(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := New(pool)

	spec, _ := ParseSchedule("every", "", 5, time.Time{}, "Europe/Rome")
	due, err := s.CreateTask(ctx, CreateTaskParams{
		Kind: KindReminder, Spec: spec, NextRunAt: time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() { cleanupTask(t, pool, due.ID) })

	for _, limit := range []int{0, -5, math.MaxInt32 + 1} {
		rows, err := s.DueTasks(ctx, limit)
		if err != nil {
			t.Fatalf("DueTasks(%d) errored, want clamp-to-1 success: %v", limit, err)
		}
		var found bool
		for _, r := range rows {
			if r.ID == due.ID {
				found = true
			}
		}
		if !found {
			t.Errorf("DueTasks(%d) did not return the overdue task (clamp failed)", limit)
		}
	}
}

func TestListUpdateCancelAndHeartbeat(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := New(pool)

	spec, _ := ParseSchedule("every", "", 5, time.Time{}, "Europe/Rome")
	first := time.Now().Add(5 * time.Minute).Truncate(time.Second)
	second := first.Add(5 * time.Minute)
	soon, err := s.CreateTask(ctx, CreateTaskParams{Kind: KindReminder, Spec: spec, NextRunAt: first})
	if err != nil {
		t.Fatalf("CreateTask soon: %v", err)
	}
	t.Cleanup(func() { cleanupTask(t, pool, soon.ID) })
	later, err := s.CreateTask(ctx, CreateTaskParams{Kind: KindBackupPostgres, Spec: spec, NextRunAt: second})
	if err != nil {
		t.Fatalf("CreateTask later: %v", err)
	}
	t.Cleanup(func() { cleanupTask(t, pool, later.ID) })
	cancelled, err := s.CreateTask(ctx, CreateTaskParams{Kind: KindBackupPostgres, Spec: spec, NextRunAt: first.Add(time.Minute)})
	if err != nil {
		t.Fatalf("CreateTask cancelled: %v", err)
	}
	t.Cleanup(func() { cleanupTask(t, pool, cancelled.ID) })

	if err := s.CancelTask(ctx, cancelled.ID); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	cancelledGot, err := s.GetTask(ctx, cancelled.ID)
	if err != nil {
		t.Fatalf("GetTask cancelled: %v", err)
	}
	if cancelledGot.Status != "cancelled" {
		t.Fatalf("cancelled task status = %q, want cancelled", cancelledGot.Status)
	}

	active, err := s.ListActiveTasks(ctx)
	if err != nil {
		t.Fatalf("ListActiveTasks: %v", err)
	}
	soonIdx, laterIdx := taskIndex(active, soon.ID), taskIndex(active, later.ID)
	if soonIdx < 0 || laterIdx < 0 {
		t.Fatalf("created active tasks not listed: soon=%d later=%d list=%+v", soonIdx, laterIdx, active)
	}
	if taskIndex(active, cancelled.ID) >= 0 {
		t.Fatalf("cancelled task appeared in active list: %+v", active)
	}
	if soonIdx > laterIdx {
		t.Fatalf("active tasks not ordered by next_run_at: soon index %d, later index %d", soonIdx, laterIdx)
	}

	advanced := first.Add(30 * time.Minute)
	if err := s.UpdateNextRunAt(ctx, soon.ID, advanced); err != nil {
		t.Fatalf("UpdateNextRunAt: %v", err)
	}
	soonGot, err := s.GetTask(ctx, soon.ID)
	if err != nil {
		t.Fatalf("GetTask soon after update: %v", err)
	}
	if !soonGot.NextRunAt.Equal(advanced.UTC()) {
		t.Fatalf("updated next_run_at = %s, want %s", soonGot.NextRunAt, advanced.UTC())
	}

	run, err := s.InsertRun(ctx, soon.ID, 7)
	if err != nil {
		t.Fatalf("InsertRun: %v", err)
	}
	var before time.Time
	if err := pool.QueryRow(ctx, "SELECT last_heartbeat_at FROM aura.agent_job_runs WHERE id = $1", run.ID).Scan(&before); err != nil {
		t.Fatalf("read heartbeat before: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if err := s.Heartbeat(ctx, run.ID); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	var after time.Time
	if err := pool.QueryRow(ctx, "SELECT last_heartbeat_at FROM aura.agent_job_runs WHERE id = $1", run.ID).Scan(&after); err != nil {
		t.Fatalf("read heartbeat after: %v", err)
	}
	if !after.After(before) {
		t.Fatalf("heartbeat did not advance timestamp: before=%s after=%s", before, after)
	}
}

func TestStoreRejectsInvalidUUIDs(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := New(pool)

	badID := "not-a-uuid"
	if err := s.CancelTask(ctx, badID); err == nil {
		t.Fatal("CancelTask accepted invalid UUID")
	}
	if err := s.UpdateNextRunAt(ctx, badID, time.Now()); err == nil {
		t.Fatal("UpdateNextRunAt accepted invalid UUID")
	}
	if _, err := s.InsertRun(ctx, badID, 1); err == nil {
		t.Fatal("InsertRun accepted invalid UUID")
	}
	if _, err := s.CreateRunAndAdvance(ctx, badID, 1, time.Now()); err == nil {
		t.Fatal("CreateRunAndAdvance accepted invalid UUID")
	}
	if err := s.Heartbeat(ctx, badID); err == nil {
		t.Fatal("Heartbeat accepted invalid UUID")
	}
	if err := s.CompleteRun(ctx, CompleteRunParams{RunID: badID, Status: "completed"}); err == nil {
		t.Fatal("CompleteRun accepted invalid UUID")
	}
}

func taskIndex(tasks []Task, id string) int {
	for i, task := range tasks {
		if task.ID == id {
			return i
		}
	}
	return -1
}
