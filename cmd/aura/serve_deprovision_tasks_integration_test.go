//go:build db_integration

package main

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The saga over the real adapter and a real Postgres. The purge takes the id alone, the shape of
// `aura identity purge --resume-resources`.
func TestDeprovisionPausesThenDeletesAnIdentitysTasks(t *testing.T) {
	if os.Getenv("AURA_DB_URL") == "" && os.Getenv("POSTGRES_PASSWORD") == "" {
		msg := "deprovision integration needs Postgres (AURA_DB_URL or POSTGRES_PASSWORD)"
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

	owner := uuid.Must(uuid.NewV7()).String()
	id := insertOwnedTask(t, pool, owner, "reminder")
	tasks := scheduledTaskAdapter{store: cron.New(pool)}
	saga := agui.NewDeprovisioner(agui.DeprovisionDeps{Jobs: tasks, ScheduledTasks: tasks})

	if err := saga.Deactivate(ctx, owner); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	var status, reason string
	if err := pool.QueryRow(ctx, `SELECT status, paused_reason FROM aura.scheduler_tasks WHERE id = $1`, id).Scan(&status, &reason); err != nil {
		t.Fatalf("read task: %v", err)
	}
	if status != "paused" || reason != cron.PausedByIdentityDeactivated {
		t.Fatalf("after Deactivate = %s/%s", status, reason)
	}

	if err := saga.Purge(ctx, agui.DeprovisionTarget{IdentityID: owner}); err != nil {
		t.Fatalf("Purge: %v", err)
	}
	err = pool.QueryRow(ctx, `SELECT status FROM aura.scheduler_tasks WHERE id = $1`, id).Scan(&status)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("after Purge: status %q, err %v", status, err)
	}
}
