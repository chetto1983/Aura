//go:build db_integration

package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/messagedrafts"
)

// mountedWhatsApp is the send tool the bridge mounts from the WhatsApp recipe.
type mountedWhatsApp struct{}

func (mountedWhatsApp) Spec() tools.Spec {
	return tools.Spec{Name: "whatsapp__send_message", TrustedRecipeSource: "recipe:whatsapp", TrustedRecipeTool: "send_message"}
}
func (mountedWhatsApp) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

func TestAScheduledMessageDraftIsStoredForItsTask(t *testing.T) {
	pool := bootstrapMigratedPool(t)
	ctx := context.Background()
	owner := uuid.NewString()
	if _, err := pool.Exec(ctx, `INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')`, owner, "message-"+owner); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id = $1`, owner) })
	sendAt := time.Now().Add(2 * time.Hour).Truncate(time.Millisecond)
	task, err := cron.New(pool).CreateTask(ctx, cron.CreateTaskParams{
		Kind: cron.KindMessage, Spec: cron.ScheduleSpec{Kind: cron.KindAt, RunAt: sendAt, TZ: "UTC"},
		Payload: []byte(`{}`), NextRunAt: sendAt, NotifyRoute: string(cron.RouteNone),
		Status: "pending_approval", IdentityID: owner,
	})
	if err != nil {
		t.Fatalf("seed task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.scheduler_tasks WHERE id = $1`, task.ID)
	})

	reg := tools.NewRegistry()
	reg.Register(mountedWhatsApp{})
	store := messagedrafts.NewStore(pool)
	draftID, err := scheduledMessageDrafts{store: store, reg: reg}.CreateScheduledMessageDraft(identityctx.WithIdentityID(ctx, owner), tools.ScheduledMessageDraft{
		ConversationID: "conv-" + owner[:8], ToolCallID: "call-1", TaskID: task.ID, Channel: "whatsapp",
		Args: json.RawMessage(`{"recipient":"10000000000","message":"hello"}`), SendAt: sendAt,
	})
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	draft, err := store.Get(ctx, owner, draftID)
	if err != nil {
		t.Fatalf("get draft: %v", err)
	}
	if draft.ScheduledTaskID != task.ID || !draft.SendAt.Equal(sendAt) ||
		!draft.ExpiresAt.Equal(sendAt.Add(tools.ScheduledMessageGrace)) || draft.RegisteredToolName != "whatsapp__send_message" {
		t.Fatalf("draft = task %q send %v expires %v tool %q", draft.ScheduledTaskID, draft.SendAt, draft.ExpiresAt, draft.RegisteredToolName)
	}
}
