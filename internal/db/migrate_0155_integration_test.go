//go:build db_integration

package db

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMigrate0155AdmitsTheScheduledMessage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin, migrateURL, _ := fresh0093Database(t, ctx, "aura_migrate0155_message")
	migrateToVersion(t, ctx, migrateURL, admin, 155)
	var identityID string
	if err := admin.QueryRow(ctx, `SELECT id::text FROM aura.identities LIMIT 1`).Scan(&identityID); err != nil {
		t.Fatal(err)
	}
	conversation := uuid.NewString()
	insertTask := func(callID any) (string, error) {
		id := uuid.NewString()
		_, err := admin.Exec(ctx, `INSERT INTO aura.scheduler_tasks
(id, kind, schedule_kind, notify_route, identity_id, origin_conversation_id, origin_tool_call_id)
VALUES ($1, 'message', 'at', 'none', $2, $3, $4)`, id, identityID, conversation, callID)
		return id, err
	}
	insertDraft := func(call string, taskID any, sendAt any, expiresAt time.Time, status string, effective any) error {
		_, err := admin.Exec(ctx, `INSERT INTO aura.message_drafts
(identity_id, conversation_id, tool_call_id, recipe, tool_name, registered_tool_name, action,
 original_args, original_fingerprint, expires_at, scheduled_task_id, send_at, status,
 effective_args, effective_fingerprint)
VALUES ($1, 'c', $2, 'recipe:whatsapp', 'send_message', 'wa__send_message', '', '{}', repeat('a', 64),
 $3, $4, $5, $6, $7, CASE WHEN $7::jsonb IS NULL THEN NULL ELSE repeat('b', 64) END)`,
			identityID, call, expiresAt, taskID, sendAt, status, effective)
		return err
	}
	sendAt := time.Now().Add(time.Hour)

	taskID, err := insertTask("call-1")
	if err != nil {
		t.Fatalf("a message task was refused: %v", err)
	}
	if err := insertDraft("d1", nil, sendAt, sendAt.Add(15*time.Minute), "pending", nil); err != nil {
		t.Fatalf("a draft with send_at and no task was refused: %v", err)
	}
	if err := insertDraft("d2", taskID, nil, sendAt, "pending", nil); err == nil {
		t.Fatal("a draft with a task and no send_at was admitted")
	}
	if err := insertDraft("d3", nil, sendAt, sendAt, "pending", nil); err == nil {
		t.Fatal("a draft expiring at its send time was admitted")
	}
	if err := insertDraft("d4", nil, sendAt, sendAt.Add(time.Minute), "approved", nil); err == nil {
		t.Fatal("an approved draft without effective args was admitted")
	}
	if err := insertDraft("d5", taskID, sendAt, sendAt.Add(time.Minute), "approved", `{}`); err != nil {
		t.Fatalf("an approved scheduled draft was refused: %v", err)
	}
	if _, err := admin.Exec(ctx, `DELETE FROM aura.scheduler_tasks WHERE id = $1`, taskID); err != nil {
		t.Fatal(err)
	}
	var unlinked bool
	if err := admin.QueryRow(ctx, `SELECT scheduled_task_id IS NULL FROM aura.message_drafts WHERE tool_call_id = 'd5'`).Scan(&unlinked); err != nil || !unlinked {
		t.Fatalf("deleting the task: draft unlinked %v, %v", unlinked, err)
	}
	if _, err := insertTask("call-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := insertTask("call-2"); err == nil {
		t.Fatal("two tasks parked by the same call were admitted")
	}
	for range 2 {
		if _, err := insertTask(nil); err != nil {
			t.Fatalf("a task with no parking call was refused: %v", err)
		}
	}

	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatal(err)
	}
	var messages int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM aura.scheduler_tasks WHERE kind = 'message'`).Scan(&messages); err != nil || messages != 0 {
		t.Fatalf("rollback kept %d message tasks, %v", messages, err)
	}
	for _, gone := range []string{
		`SELECT send_at FROM aura.message_drafts`,
		`SELECT scheduled_task_id FROM aura.message_drafts`,
		`SELECT origin_tool_call_id FROM aura.scheduler_tasks`,
	} {
		if _, err := admin.Exec(ctx, gone); err == nil {
			t.Fatalf("rollback retained: %s", gone)
		}
	}
}
