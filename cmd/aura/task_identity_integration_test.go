//go:build db_integration

package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/db"
	"github.com/google/uuid"
)

// TestCLIScheduledTaskCarriesAnOwner is the regression for a defect that was invisible to
// every unit test here and that only showed up while driving a real delivery: the CLI's
// INSERT omitted identity_id, so the column took its 'local' default (migration 0009) and
// the dispatcher's origin gate — which treats 'local' as "no channel owns this" — could
// never deliver a CLI-scheduled task to Telegram or anywhere else. It failed silently,
// with --notify telegram accepted and stored.
//
// The assertion is deliberately about the OWNER, not about delivery: a row that owns
// nothing is the precondition for the whole class.
func TestCLIScheduledTaskCarriesAnOwner(t *testing.T) {
	if os.Getenv("AURA_DB_URL") == "" && os.Getenv("POSTGRES_PASSWORD") == "" {
		msg := "task identity integration needs Postgres (AURA_DB_URL or POSTGRES_PASSWORD)"
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
	// Registered FIRST so it runs LAST: t.Cleanup is LIFO and every deferred call in this
	// function runs BEFORE any cleanup. A plain `defer pool.Close()` closed the pool out
	// from under the row-delete below, which then failed and only logged it — leaving the
	// row behind while the test still reported success.
	t.Cleanup(pool.Close)

	// A synthetic owner on purpose: this asserts the COLUMN is written, which is what the
	// bug was. Resolving the real operator here would tie the test to a database that has
	// one, and the standing rule is that db_integration runs against a disposable DB.
	owner := uuid.Must(uuid.NewV7()).String()

	id := uuid.Must(uuid.NewV7()).String()
	row := scheduledTaskRow{
		ID:           id,
		Kind:         "reminder",
		ScheduleKind: "at",
		Spec:         cronSpecAt(t, time.Now().Add(24*time.Hour)),
		Payload:      []byte(`{"text":"owner regression"}`),
		Status:       "active",
		NextRunAt:    time.Now().Add(24 * time.Hour),
		NotifyRoute:  "telegram",
		IdentityID:   owner,
	}
	if err := insertScheduledTask(ctx, pool, row); err != nil {
		t.Fatalf("insert scheduled task: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM aura.scheduler_tasks WHERE id = $1`, id); err != nil {
			t.Logf("cleanup scheduled task %s: %v", id, err)
		}
	})

	var stored, route string
	if err := pool.QueryRow(ctx,
		`SELECT identity_id, notify_route FROM aura.scheduler_tasks WHERE id = $1`, id,
	).Scan(&stored, &route); err != nil {
		t.Fatalf("read back scheduled task: %v", err)
	}
	if stored != owner {
		t.Errorf("stored identity_id = %q, want the resolved operator %q", stored, owner)
	}
	if stored == "local" {
		t.Error("stored identity_id fell back to 'local': the origin gate will refuse every channel delivery for this task")
	}
	if route != "telegram" {
		t.Errorf("stored notify_route = %q, want telegram", route)
	}
}

func cronSpecAt(t *testing.T, when time.Time) (spec cron.ScheduleSpec) {
	t.Helper()
	spec.RunAt = when.UTC()
	spec.TZ = "Europe/Rome"
	return spec
}
