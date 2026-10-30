//go:build db_integration

package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/onboarding"
)

// A cron task scheduled with no tz by an identity whose profile says New York fires at 07:30 New
// York time (docs/superpowers/specs/2026-10-09-daily-briefing-design.md, Testing).
func TestACronWithoutTZFiresOnTheIdentitysWallClock(t *testing.T) {
	pool := bootstrapMigratedPool(t)
	ctx := context.Background()
	owner := uuid.NewString()
	if _, err := pool.Exec(ctx, `INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')`, owner, "zone-"+owner); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id = $1`, owner) })
	if err := onboarding.NewProfileStore(pool).Save(ctx, owner, onboarding.Answers{Timezone: "America/New_York"}); err != nil {
		t.Fatalf("save the profile: %v", err)
	}

	tool := newTaskTool(&cronTaskStore{pool: pool, store: cron.New(pool)}, nil, "Asia/Tokyo")
	args := `{"action":"schedule","schedule_kind":"cron","cron":"30 7 * * *","kind":"reminder","payload":{"text":"briefing"},"notify":"stdout"}`
	if _, err := tool.Execute(identityctx.WithIdentityID(ctx, owner), json.RawMessage(args)); err != nil {
		t.Fatalf("schedule: %v", err)
	}

	var zone string
	var next time.Time
	if err := pool.QueryRow(ctx, `SELECT tz, next_run_at FROM aura.scheduler_tasks WHERE identity_id = $1`, owner).Scan(&zone, &next); err != nil {
		t.Fatalf("read the task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.scheduler_tasks WHERE identity_id = $1`, owner)
	})
	newYork, _ := time.LoadLocation("America/New_York")
	if local := next.In(newYork); zone != "America/New_York" || local.Hour() != 7 || local.Minute() != 30 {
		t.Fatalf("tz=%q next_run_at=%s (New York %s), want America/New_York at 07:30", zone, next, local)
	}
}
