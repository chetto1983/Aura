//go:build db_integration

package main

import (
	"context"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestTaskToolHistoryIsOwnerScoped drives the history action's adapter against Postgres: the
// owner reads its runs newest first, a cancelled task's runs stay readable (the task replaced
// on 2026-11-28 was cancelled), and another identity reads nothing.
func TestTaskToolHistoryIsOwnerScoped(t *testing.T) {
	requireConfiguredPostgres(t, "task history integration")
	ctx := context.Background()
	cfg := config.LoadDB()
	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	adapter := &cronTaskStore{pool: pool, store: cron.New(pool)}

	owner := uuid.Must(uuid.NewV7()).String()
	ownerCtx := identityctx.WithIdentityID(ctx, owner)
	job := insertOwnedTask(t, pool, owner, "agent_job")
	start := time.Date(2026, 11, 28, 11, 7, 56, 0, time.UTC)
	insertRun(t, pool, job, start, "completed", "Nessuna anomalia rilevata", "")
	insertRun(t, pool, job, start.Add(3*time.Minute), "failed", "", "model timeout")

	runs, err := adapter.ListTaskRuns(ownerCtx, job, 5)
	if err != nil {
		t.Fatalf("owner history: %v", err)
	}
	if len(runs) != 2 || runs[0].Status != "failed" || runs[0].LastError != "model timeout" ||
		runs[1].Summary != "Nessuna anomalia rilevata" || !runs[1].StartedAt.Equal(start) {
		t.Fatalf("owner history = %+v, want the failed run first, then the completed one", runs)
	}
	if runs, err := adapter.ListTaskRuns(ownerCtx, job, 1); err != nil || len(runs) != 1 {
		t.Fatalf("history limit 1 = %+v, %v; want one run", runs, err)
	}

	cancelled := insertOwnedTask(t, pool, owner, "agent_job")
	insertRun(t, pool, cancelled, start, "completed", "report", "")
	if _, err := pool.Exec(ctx, `UPDATE aura.scheduler_tasks SET status = 'cancelled' WHERE id = $1`, cancelled); err != nil {
		t.Fatalf("cancel task: %v", err)
	}
	if runs, err := adapter.ListTaskRuns(ownerCtx, cancelled, 5); err != nil || len(runs) != 1 {
		t.Fatalf("cancelled task history = %+v, %v; want its run", runs, err)
	}

	stranger := identityctx.WithIdentityID(ctx, uuid.Must(uuid.NewV7()).String())
	if runs, err := adapter.ListTaskRuns(stranger, job, 5); err != nil || len(runs) != 0 {
		t.Fatalf("another identity's history = %+v, %v; want no runs", runs, err)
	}
}

func insertRun(t *testing.T, pool *pgxpool.Pool, taskID string, started time.Time, status, summary, lastError string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO aura.agent_job_runs (id, task_id, status, started_at, summary, last_error)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''))`,
		uuid.Must(uuid.NewV7()).String(), taskID, status, started, summary, lastError); err != nil {
		t.Fatalf("insert run: %v", err)
	}
}
