//go:build db_integration

package main

// serve_scheduler_scope_integration_test.go covers the scheduler store's identity scoping
// against the REAL schema, because that is the only place the defect it guards could be seen.
//
// Measured 2026-09-06 on a live stack, driving the agent as the model: every `task` action
// answered
//
//	ERROR: operator does not exist: text = uuid (SQLSTATE 42883)
//
// aura.scheduler_tasks.identity_id is TEXT, and all four queries compared it with $n::uuid.
// Postgres has no text = uuid operator, so list, cancel and run_now were broken for every
// caller. Nothing caught it: the queries are hand-written rather than sqlc-generated, so no
// build step ever compared them against the column types, and no test ran them against a
// migrated database.
//
// The scoping matters as much as the syntax. scheduler_tasks carries NO row-level security —
// measured: zero policies — so the WHERE clause is the ONLY thing keeping one identity's
// schedule out of another's, which is exactly the arrangement the project's own rules warn
// about. A test that only asserted "no error" would have let a dropped WHERE through.

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/dbtest"
	"github.com/chetto1983/aura/internal/identityctx"
)

func schedulerScopePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	migrateURL := dbtest.MigrateURL(t, skillsBridgeEnvOrSkip(t, "AURA_DB_MIGRATE_URL"))
	appURL := skillsBridgeEnvOrSkip(t, "AURA_DB_URL")
	pwd := skillsBridgeEnvOrSkip(t, "POSTGRES_PASSWORD")

	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	if err := db.EnsureRoles(ctx, "postgres://aura:"+pwd+"@"+host+":"+port+"/aura?sslmode=disable", pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	if _, err := db.Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := db.Open(ctx, &db.Config{URL: appURL})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// seedSchedulerTask inserts one task owned by owner and removes it when the test ends.
func seedSchedulerTask(t *testing.T, pool *pgxpool.Pool, owner, payload string) {
	t.Helper()
	ctx := context.Background()
	id := uuid.NewString()
	_, err := pool.Exec(ctx, `
		INSERT INTO aura.scheduler_tasks
			(id, identity_id, kind, schedule_kind, status, payload, next_run_at, run_at, notify_route, tz)
		VALUES ($1::uuid, $2, 'reminder', 'at', 'active', $3::jsonb, now() + interval '1 day',
		        now() + interval '1 day', 'none', 'Europe/Rome')`, id, owner, payload)
	if err != nil {
		t.Fatalf("seed task for %s: %v", owner, err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.scheduler_tasks WHERE id = $1::uuid`, id)
	})
}

// TestListScheduledTasksRunsAndStaysInsideTheIdentity is both halves at once: the query has to
// EXECUTE against the real column types, and it has to return this identity's rows only.
func TestListScheduledTasksRunsAndStaysInsideTheIdentity(t *testing.T) {
	pool := schedulerScopePool(t)
	store := &cronTaskStore{pool: pool}

	mine := uuid.NewString()
	theirs := uuid.NewString()
	seedSchedulerTask(t, pool, mine, `{"text":"mine"}`)
	seedSchedulerTask(t, pool, theirs, `{"text":"theirs"}`)

	ctx := identityctx.WithIdentityID(context.Background(), mine)
	got, err := store.ListScheduledTasks(ctx)
	if err != nil {
		t.Fatalf("ListScheduledTasks: %v", err)
	}
	// Non-vacuous by construction: the seed above guarantees the other identity HAS a row, so
	// an empty answer here cannot be mistaken for isolation.
	if len(got) != 1 {
		t.Fatalf("want exactly this identity's 1 task, got %d: %+v", len(got), got)
	}
	// Substring rather than equality: Postgres normalises jsonb on the way out (it returns
	// `{"text": "mine"}`, with the space), so pinning the exact bytes would test the driver's
	// formatting instead of the scoping.
	if !strings.Contains(got[0].Payload, "mine") || strings.Contains(got[0].Payload, "theirs") {
		t.Fatalf("returned another identity's task: %+v", got[0])
	}
}
