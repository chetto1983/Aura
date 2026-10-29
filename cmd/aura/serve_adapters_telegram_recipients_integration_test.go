//go:build db_integration

package main

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/channels/telegram"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/identityctx"
)

// The live task tool reads the caller's Telegram links from Postgres, asks when there are two,
// and persists and lists the accounts the operator chose (spec
// 2026-10-28-telegram-push-recipients, R2 and acceptance 5).
func TestTaskToolPersistsTheChosenTelegramRecipients(t *testing.T) {
	pool := bootstrapMigratedPool(t)
	ctx := context.Background()
	owner := uuid.NewString()
	if _, err := pool.Exec(ctx, `INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')`, owner, "recipients-"+owner); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id = $1`, owner) })
	links := telegram.New(pool)
	const phone, work int64 = 9_000_145_201, 9_000_145_202
	for _, chat := range []int64{phone, work} {
		token := uuid.NewString()
		if err := links.InsertPending(ctx, telegram.InsertPendingParams{OnboardingToken: token, IdentityID: owner, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
			t.Fatalf("mint a token for %d: %v", chat, err)
		}
		if _, err := links.ConsumeOnboarding(ctx, telegram.ConsumeParams{OnboardingToken: token, TelegramUserID: chat}); err != nil {
			t.Fatalf("link %d: %v", chat, err)
		}
	}

	store := &cronTaskStore{pool: pool, store: cron.New(pool)}
	tool := newTaskTool(store, nil)
	ownerCtx := identityctx.WithIdentityID(ctx, owner)
	schedule := func(extra string) string {
		t.Helper()
		args := `{"action":"schedule","schedule_kind":"at","at":"2030-01-01T09:30:00Z","kind":"reminder","payload":{"text":"drink water"},"notify":"telegram"` + extra + `}`
		res, err := tool.Execute(ownerCtx, json.RawMessage(args))
		if err != nil {
			t.Fatalf("schedule%s: %v", extra, err)
		}
		return res.Preview
	}

	if got := schedule(""); !strings.Contains(got, `"status":"telegram_recipients_required"`) {
		t.Fatalf("two links and none named = %s, want the choice directive", got)
	}
	schedule(`,"telegram_recipients":[9000145202,9000145201]`)
	listed, err := store.ListScheduledTasks(ownerCtx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 || !slices.Equal(listed[0].TelegramRecipients, []int64{work, phone}) {
		t.Fatalf("listed %+v, want one task naming %d then %d", listed, work, phone)
	}
	taskID := listed[0].ID
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.scheduler_tasks WHERE id = $1`, taskID)
	})

	if _, err := pool.Exec(ctx, `UPDATE aura.scheduler_tasks SET telegram_recipients = NULL WHERE id = $1`, taskID); err != nil {
		t.Fatalf("clear the recipients: %v", err)
	}
	listed, err = store.ListScheduledTasks(ownerCtx)
	if err != nil {
		t.Fatalf("list after clearing: %v", err)
	}
	if listed[0].TelegramRecipients != nil {
		t.Fatalf("a task that names none listed %v, want nil", listed[0].TelegramRecipients)
	}
}
