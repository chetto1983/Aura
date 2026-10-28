//go:build db_integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// 0145 lets a scheduled task name the Telegram accounts its pushes reach, and a pending push name
// the one account it is for (docs/superpowers/specs/2026-10-28-telegram-push-recipients-design.md,
// R1 and R4). A named list is never empty.
func TestMigrate0145NamesTelegramRecipients(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, migrateURL, _ := fresh0093Database(t, ctx, "aura_migrate0145_recipients")
	migrateToVersion(t, ctx, migrateURL, admin, 144)
	assert0145Columns(t, ctx, admin, false)

	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0145 up: %v", err)
	}
	assert0145Columns(t, ctx, admin, true)
	insert := func(recipients any) error {
		_, err := admin.Exec(ctx, `
INSERT INTO aura.scheduler_tasks (id, kind, schedule_kind, notify_route, telegram_recipients)
VALUES ($1, 'reminder', 'at', 'telegram', $2)`, uuid.NewString(), recipients)
		return err
	}
	for name, recipients := range map[string]any{"none named": nil, "two named": []int64{9_000_140_001, 9_000_140_002}} {
		if err := insert(recipients); err != nil {
			t.Errorf("%s was refused: %v", name, err)
		}
	}
	if insert([]int64{}) == nil {
		t.Error("an empty recipient list was accepted, want the CHECK to refuse it")
	}

	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("migrate 0145 down: %v", err)
	}
	assert0145Columns(t, ctx, admin, false)
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0145 up again: %v", err)
	}
	assert0145Columns(t, ctx, admin, true)
}

func assert0145Columns(t *testing.T, ctx context.Context, admin *pgxpool.Pool, want bool) {
	t.Helper()
	for _, c := range []struct{ table, column string }{
		{"scheduler_tasks", "telegram_recipients"},
		{"pending_notifications", "telegram_user_id"},
	} {
		assertColumnExists(t, ctx, admin, c.table, c.column, want)
	}
}
