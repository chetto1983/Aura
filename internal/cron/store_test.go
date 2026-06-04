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
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
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

// migratedPool ensures roles + migrations are applied, then returns an aura_app
// pool ready for Store use. Closed via t.Cleanup.
func migratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := envOrSkip(t, "AURA_DB_MIGRATE_URL")
	appURL := envOrSkip(t, "AURA_DB_URL")

	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	bootstrap := fmt.Sprintf("postgres://aura:%s@%s:%s/aura?sslmode=disable", pwd, host, port)

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
