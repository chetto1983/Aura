//go:build db_integration

package messagedrafts

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
)

// scheduledTask inserts the message task a scheduled draft belongs to.
func scheduledTask(t *testing.T, pool *pgxpool.Pool, owner string) string {
	t.Helper()
	id := uuid.NewString()
	err := db.WithIdentityTxRaw(context.Background(), pool, owner, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO aura.scheduler_tasks
(id, kind, schedule_kind, notify_route, identity_id, status) VALUES ($1, 'message', 'at', 'none', $2, 'pending_approval')`, id, owner)
		return err
	})
	if err != nil {
		t.Fatalf("seed message task: %v", err)
	}
	t.Cleanup(func() {
		_ = db.WithIdentityTxRaw(context.Background(), pool, owner, func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), `DELETE FROM aura.scheduler_tasks WHERE id = $1`, id)
			return err
		})
	})
	return id
}

func scheduledInput(owner, call, taskID string, sendAt time.Time) DraftInput {
	return DraftInput{
		IdentityID: owner, ConversationID: "scheduled-conversation", ToolCallID: call,
		Target:             Target{Recipe: "recipe:whatsapp", Tool: "send_message"},
		RegisteredToolName: "whatsapp__send_message",
		OriginalArgs:       json.RawMessage(`{"recipient":"10000000000","message":"hello"}`),
		ExpiresAt:          sendAt.Add(15 * time.Minute),
		ScheduledTaskID:    taskID,
		SendAt:             sendAt,
	}
}

func TestAScheduledDraftIsCreatedBoundToItsTask(t *testing.T) {
	pool := draftTestPool(t)
	owner := draftTestIdentity(t, pool)
	taskID := scheduledTask(t, pool, owner)
	sendAt := time.Now().Add(time.Hour)
	store := NewStore(pool)

	draft, err := store.Create(context.Background(), scheduledInput(owner, "call-bound", taskID, sendAt))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := store.Get(context.Background(), owner, draft.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ScheduledTaskID != taskID || got.SendAt.Sub(sendAt).Abs() > time.Millisecond {
		t.Fatalf("got task %q at %v, want %q at %v", got.ScheduledTaskID, got.SendAt, taskID, sendAt)
	}
}

// Decision 30: the same call bound to another task is a different draft, not a retry.
func TestADraftForAnotherTaskIsADuplicate(t *testing.T) {
	pool := draftTestPool(t)
	owner := draftTestIdentity(t, pool)
	sendAt := time.Now().Add(time.Hour)
	store := NewStore(pool)

	if _, err := store.Create(context.Background(), scheduledInput(owner, "call-dup", scheduledTask(t, pool, owner), sendAt)); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := store.Create(context.Background(), scheduledInput(owner, "call-dup", scheduledTask(t, pool, owner), sendAt))
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("err = %v, want ErrDuplicate", err)
	}
}

// A scheduled draft is sent by its task at its time, never by the immediate-send claim.
func TestClaimSendRefusesAScheduledDraft(t *testing.T) {
	pool := draftTestPool(t)
	owner := draftTestIdentity(t, pool)
	store := NewStore(pool)
	draft, err := store.Create(context.Background(), scheduledInput(owner, "call-claim", scheduledTask(t, pool, owner), time.Now().Add(time.Hour)))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.ClaimSend(context.Background(), owner, draft.ID, nil); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("ClaimSend = %v, want ErrUnavailable", err)
	}
	got, err := store.Get(context.Background(), owner, draft.ID)
	if err != nil || got.Status != StatusPending {
		t.Fatalf("after the refused claim: %v, %v; want pending", got.Status, err)
	}
}

func TestApproveFixesTheEditedArgumentsOnce(t *testing.T) {
	pool := draftTestPool(t)
	owner := draftTestIdentity(t, pool)
	store := NewStore(pool)
	draft, err := store.Create(context.Background(), scheduledInput(owner, "call-approve", scheduledTask(t, pool, owner), time.Now().Add(time.Hour)))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	approved, err := store.Approve(context.Background(), owner, draft.ID, json.RawMessage(`{"message":"Edited"}`))
	if err != nil || approved.Status != StatusApproved || len(approved.EffectiveFingerprint) != 64 {
		t.Fatalf("approve = %+v, %v", approved, err)
	}
	var args map[string]string
	if err := json.Unmarshal(approved.EffectiveArgs, &args); err != nil || args["message"] != "Edited" {
		t.Fatalf("effective args = %s, %v", approved.EffectiveArgs, err)
	}
	if _, err := store.Approve(context.Background(), owner, draft.ID, nil); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("second approve = %v, want ErrUnavailable", err)
	}
}

func TestApproveRefusesAfterTheSendTime(t *testing.T) {
	pool := draftTestPool(t)
	owner := draftTestIdentity(t, pool)
	store := NewStore(pool)
	draft, err := store.Create(context.Background(), scheduledInput(owner, "call-late", scheduledTask(t, pool, owner), time.Now().Add(time.Hour)))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := db.WithIdentityTxRaw(context.Background(), pool, owner, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE aura.message_drafts SET send_at = now() - interval '1 second' WHERE id = $1`, draft.ID)
		return err
	}); err != nil {
		t.Fatalf("move send_at: %v", err)
	}
	if _, err := store.Approve(context.Background(), owner, draft.ID, nil); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("approve after the send time = %v, want ErrUnavailable", err)
	}
	if got, _ := store.Get(context.Background(), owner, draft.ID); got.Status != StatusPending {
		t.Fatalf("status = %q, want pending", got.Status)
	}
}

func TestApproveRefusesAnImmediateDraft(t *testing.T) {
	pool := draftTestPool(t)
	owner := draftTestIdentity(t, pool)
	store := NewStore(pool)
	input := scheduledInput(owner, "call-now", "", time.Time{})
	input.ExpiresAt = time.Now().Add(time.Hour)
	draft, err := store.Create(context.Background(), input)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.Approve(context.Background(), owner, draft.ID, nil); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("approve an immediate draft = %v, want ErrUnavailable", err)
	}
}

// A cancel declines what has not been sent; a draft already claimed is left to its outcome.
func TestDeclineForTaskDeclinesOnlyAnUnsentDraft(t *testing.T) {
	pool := draftTestPool(t)
	owner := draftTestIdentity(t, pool)
	store := NewStore(pool)
	sendAt := time.Now().Add(time.Hour)
	statusOf := func(id string) Status {
		got, err := store.Get(context.Background(), owner, id)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		return got.Status
	}
	for name, prepare := range map[string]func(Draft){
		"pending":  func(Draft) {},
		"approved": func(d Draft) { _, _ = store.Approve(context.Background(), owner, d.ID, nil) },
	} {
		taskID := scheduledTask(t, pool, owner)
		draft, err := store.Create(context.Background(), scheduledInput(owner, "call-"+name, taskID, sendAt))
		if err != nil {
			t.Fatalf("%s: create: %v", name, err)
		}
		prepare(draft)
		if err := store.DeclineForTask(context.Background(), owner, taskID); err != nil {
			t.Fatalf("%s: decline: %v", name, err)
		}
		if got := statusOf(draft.ID); got != StatusDeclined {
			t.Fatalf("%s draft = %q, want declined", name, got)
		}
	}
	taskID := scheduledTask(t, pool, owner)
	draft, err := store.Create(context.Background(), scheduledInput(owner, "call-dispatching", taskID, sendAt))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.Approve(context.Background(), owner, draft.ID, nil); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := db.WithIdentityTxRaw(context.Background(), pool, owner, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE aura.message_drafts SET status = 'dispatching', dispatch_started_at = now() WHERE id = $1`, draft.ID)
		return err
	}); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := store.DeclineForTask(context.Background(), owner, taskID); err != nil {
		t.Fatalf("decline: %v", err)
	}
	if got := statusOf(draft.ID); got != StatusDispatching {
		t.Fatalf("claimed draft = %q, want dispatching", got)
	}
}
