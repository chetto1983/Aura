//go:build db_integration

package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestTaskToolPauseIsOwnerScoped drives the task tool's adapter against Postgres: the owner
// pauses and resumes its reminder and sees it paused in its list; another identity cannot
// touch it; the database backup cannot be paused by its own owner either.
func TestTaskToolPauseIsOwnerScoped(t *testing.T) {
	if os.Getenv("AURA_DB_URL") == "" && os.Getenv("POSTGRES_PASSWORD") == "" {
		msg := "task pause integration needs Postgres (AURA_DB_URL or POSTGRES_PASSWORD)"
		if os.Getenv("CI") != "" {
			t.Fatal(msg)
		}
		t.Skip(msg)
	}
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
	reminder := insertOwnedTask(t, pool, owner, "reminder")
	backup := insertOwnedTask(t, pool, owner, "backup_postgres")

	stranger := identityctx.WithIdentityID(ctx, uuid.Must(uuid.NewV7()).String())
	if err := adapter.PauseScheduledTask(stranger, reminder); err == nil || !strings.Contains(err.Error(), "not owned") {
		t.Fatalf("another identity's pause = %v, want not owned", err)
	}
	if err := adapter.PauseScheduledTask(ownerCtx, backup); err == nil || !strings.Contains(err.Error(), "cannot be paused") {
		t.Fatalf("pausing the backup = %v, want refused", err)
	}

	if err := adapter.PauseScheduledTask(ownerCtx, reminder); err != nil {
		t.Fatalf("owner pause: %v", err)
	}
	if err := adapter.PauseScheduledTask(ownerCtx, reminder); err == nil || !strings.Contains(err.Error(), "not active") {
		t.Fatalf("second pause = %v, want not active", err)
	}
	listed, err := adapter.ListScheduledTasks(ownerCtx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !listedAs(listed, reminder, "paused", cron.PausedByOperator) {
		t.Fatalf("the owner's list must show the paused reminder: %+v", listed)
	}

	if _, err := adapter.ResumeScheduledTask(stranger, reminder); err == nil {
		t.Fatal("another identity must not resume the owner's task")
	}
	next, err := adapter.ResumeScheduledTask(ownerCtx, reminder)
	if err != nil || !next.After(time.Now()) {
		t.Fatalf("owner resume = %v, %v; want a future next fire", next, err)
	}
	if _, err := adapter.ResumeScheduledTask(ownerCtx, reminder); err == nil || !strings.Contains(err.Error(), "not paused") {
		t.Fatalf("second resume = %v, want not paused", err)
	}

	if err := adapter.PauseScheduledTask(ownerCtx, reminder); err != nil {
		t.Fatalf("pause before cancel: %v", err)
	}
	if err := adapter.CancelScheduledTask(ownerCtx, reminder); err != nil {
		t.Fatalf("cancelling a paused task: %v", err)
	}
}

func insertOwnedTask(t *testing.T, pool *pgxpool.Pool, owner, kind string) string {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	next := time.Now().Add(24 * time.Hour)
	if err := insertScheduledTask(context.Background(), pool, scheduledTaskRow{
		ID: id, Kind: kind, ScheduleKind: "at", Spec: cronSpecAt(t, next),
		Payload: []byte(`{"text":"pause"}`), Status: "active", NextRunAt: next,
		NotifyRoute: "none", IdentityID: owner,
	}); err != nil {
		t.Fatalf("insert %s task: %v", kind, err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM aura.scheduler_tasks WHERE id = $1`, id); err != nil {
			t.Logf("cleanup task %s: %v", id, err)
		}
	})
	return id
}

func listedAs(tasks []tools.ScheduledTask, id, status, reason string) bool {
	for _, task := range tasks {
		if task.ID == id && task.Status == status && task.PausedReason == reason {
			return true
		}
	}
	return false
}
