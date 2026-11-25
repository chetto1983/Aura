//go:build db_integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// 0158's down restores 0134's target CHECK, which has no shape for a send_email draft: it deletes
// those drafts first, so `aura db reset` does not stop at a dirty 158 on a database that held one.
func TestMigrate0158DownDropsTheSendEmailDrafts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin, migrateURL, _ := fresh0093Database(t, ctx, "aura_migrate0158_drafts")
	migrateToVersion(t, ctx, migrateURL, admin, 158)
	identityID := anyIdentity(t, ctx, admin)
	if err := insert0158Draft(ctx, admin, identityID, "split", "recipe:calendar", "send_email", "calendar__send_email", ""); err != nil {
		t.Fatalf("a send_email draft was refused at 158: %v", err)
	}
	if err := insert0158Draft(ctx, admin, identityID, "legacy", "recipe:calendar", "calendar", "calendar__calendar", "send_email"); err != nil {
		t.Fatalf("a multiplexed draft was refused at 158: %v", err)
	}
	if err := insert0158Draft(ctx, admin, identityID, "whatsapp", "recipe:whatsapp", "send_message", "wa__send_message", ""); err != nil {
		t.Fatalf("a whatsapp draft was refused at 158: %v", err)
	}
	taskID := uuid.NewString()
	if _, err := admin.Exec(ctx, `INSERT INTO aura.scheduler_tasks
(id, kind, schedule_kind, notify_route, identity_id, origin_conversation_id, origin_tool_call_id)
VALUES ($1, 'message', 'at', 'none', $2, $3, 'scheduled')`, taskID, identityID, uuid.NewString()); err != nil {
		t.Fatalf("insert the message task: %v", err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO aura.message_drafts
(identity_id, conversation_id, tool_call_id, recipe, tool_name, registered_tool_name, action,
 original_args, original_fingerprint, expires_at, scheduled_task_id, send_at)
VALUES ($1, 'c', 'scheduled', 'recipe:calendar', 'send_email', 'calendar__send_email', '', '{}', repeat('a', 64),
 now() + interval '2 hours', $2, now() + interval '1 hour')`, identityID, taskID); err != nil {
		t.Fatalf("a scheduled send_email draft was refused at 158: %v", err)
	}

	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("migrate 0158 down: %v", err)
	}
	if got := currentMigrationVersion(t, ctx, admin); got != 157 {
		t.Fatalf("version after 0158 down = %d, want 157", got)
	}
	var kept []string
	rows, err := admin.Query(ctx, `SELECT tool_call_id FROM aura.message_drafts WHERE identity_id = $1 ORDER BY tool_call_id`, identityID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var call string
		if err := rows.Scan(&call); err != nil {
			t.Fatal(err)
		}
		kept = append(kept, call)
	}
	if err := rows.Err(); err != nil || len(kept) != 2 || kept[0] != "legacy" || kept[1] != "whatsapp" {
		t.Fatalf("drafts after 0158 down = %v, %v; want legacy and whatsapp", kept, err)
	}
	var tasks int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM aura.scheduler_tasks WHERE id = $1`, taskID).Scan(&tasks); err != nil || tasks != 0 {
		t.Fatalf("the deleted draft's message task survived: %d, %v", tasks, err)
	}
	if err := insert0158Draft(ctx, admin, identityID, "split-2", "recipe:calendar", "send_email", "calendar__send_email", ""); err == nil {
		t.Fatal("0134's CHECK admits a send_email draft after 0158 down")
	}

	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("migrate back to head: %v", err)
	}
	if err := insert0158Draft(ctx, admin, identityID, "split-3", "recipe:calendar", "send_email", "calendar__send_email", ""); err != nil {
		t.Fatalf("a send_email draft was refused at head: %v", err)
	}
	if err := Reset(ctx, migrateURL); err != nil {
		t.Fatalf("Reset over a send_email draft: %v", err)
	}
	if n, err := Migrate(ctx, migrateURL); err != nil || n != 0 {
		t.Fatalf("post-Reset Migrate = %d, %v; want 0 pending", n, err)
	}
}

func anyIdentity(t *testing.T, ctx context.Context, admin *pgxpool.Pool) string {
	t.Helper()
	var id string
	if err := admin.QueryRow(ctx, `SELECT id::text FROM aura.identities LIMIT 1`).Scan(&id); err != nil {
		t.Fatalf("read an identity: %v", err)
	}
	return id
}

func insert0158Draft(ctx context.Context, admin *pgxpool.Pool, identityID, call, recipe, tool, registered, action string) error {
	_, err := admin.Exec(ctx, `INSERT INTO aura.message_drafts
(identity_id, conversation_id, tool_call_id, recipe, tool_name, registered_tool_name, action,
 original_args, original_fingerprint, expires_at)
VALUES ($1, 'c', $2, $3, $4, $5, $6, '{}', repeat('a', 64), now() + interval '1 hour')`,
		identityID, call, recipe, tool, registered, action)
	return err
}
