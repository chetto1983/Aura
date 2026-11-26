//go:build db_integration

package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
)

// The task tool tells the conversation's own task from another one by the origin the cancel
// target carries, and it never asks about a task the cancel would refuse: the read refuses the
// same tasks, inside the same identity.
func TestCancelTargetCarriesTheOriginAndRefusesWhatCancelRefuses(t *testing.T) {
	if os.Getenv("AURA_DB_URL") == "" && os.Getenv("POSTGRES_PASSWORD") == "" {
		msg := "task cancel integration needs Postgres (AURA_DB_URL or POSTGRES_PASSWORD)"
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

	cli := insertOwnedTask(t, pool, owner, "reminder")
	target, err := adapter.CancelTarget(ownerCtx, cli)
	if err != nil {
		t.Fatalf("cancel target of a CLI task: %v", err)
	}
	if target.ID != cli || target.Kind != "reminder" || target.ScheduleKind != "at" || target.OriginConversationID != "" ||
		target.NextRunAt.IsZero() || !strings.Contains(target.Payload, `"pause"`) {
		t.Fatalf("cancel target = %+v", target)
	}

	conversation := uuid.NewString()
	chat := insertOwnedTask(t, pool, owner, "reminder")
	if _, err := pool.Exec(ctx, `UPDATE aura.scheduler_tasks SET origin_conversation_id = $2 WHERE id = $1`, chat, conversation); err != nil {
		t.Fatalf("set the origin: %v", err)
	}
	if target, err := adapter.CancelTarget(ownerCtx, chat); err != nil || target.OriginConversationID != conversation {
		t.Fatalf("cancel target of a chat task = %+v, %v; want origin %s", target, err, conversation)
	}

	stranger := identityctx.WithIdentityID(ctx, uuid.Must(uuid.NewV7()).String())
	if _, err := adapter.CancelTarget(stranger, chat); err == nil || !strings.Contains(err.Error(), "not owned") {
		t.Fatalf("another identity's cancel target = %v, want not owned", err)
	}
	backup := insertOwnedTask(t, pool, owner, "backup_postgres")
	if _, err := adapter.CancelTarget(ownerCtx, backup); err == nil || !strings.Contains(err.Error(), "cannot be cancelled") {
		t.Fatalf("cancel target of the backup = %v, want refused", err)
	}
	watch := insertOwnedTask(t, pool, owner, "pim_watch")
	if _, err := adapter.CancelTarget(ownerCtx, watch); err == nil || !strings.Contains(err.Error(), "mail_watch") {
		t.Fatalf("cancel target of the watch = %v, want it pointed at mail_watch", err)
	}

	if err := adapter.CancelScheduledTask(ownerCtx, chat); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := adapter.CancelTarget(ownerCtx, chat); err == nil || !strings.Contains(err.Error(), "not active") {
		t.Fatalf("cancel target of a cancelled task = %v, want not active", err)
	}
}
