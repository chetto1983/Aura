//go:build db_integration

package runner

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/messagedrafts"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// A runner that dies between claiming a send and writing its outcome leaves the draft dispatching.
// After a restart the sweep closes it as uncertain, and its review answers "do not retry" without
// running the send a second time.
func TestASendCutShortByARestartResumesAsUncertainWithoutASecondSend(t *testing.T) {
	pool := migratedRunnerPool(t)
	ctx := ownerCtx()
	first, convStore, pauseStore := newIntegrationRunner(t, pool, agenttest.NewFakeClient())
	first.messageDrafts = messagedrafts.NewStore(pool)
	conversationID := newIntegrationConversation(t, pool, convStore)
	draft, err := first.messageDrafts.Create(ctx, messagedrafts.DraftInput{
		IdentityID: localIdentityID, ConversationID: conversationID, ToolCallID: "call-restart",
		Target:             messagedrafts.Target{Recipe: "recipe:whatsapp", Tool: "send_message"},
		RegisteredToolName: "whatsapp__send_message",
		OriginalArgs:       json.RawMessage(`{"recipient":"12345","message":"private text"}`),
		ExpiresAt:          time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create review draft: %v", err)
	}
	t.Cleanup(func() {
		if err := db.WithIdentityTxRaw(context.Background(), pool, localIdentityID, func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), `DELETE FROM aura.message_drafts WHERE id=$1`, draft.ID)
			return err
		}); err != nil {
			t.Errorf("clean up review draft: %v", err)
		}
	})
	if err := pauseStore.Insert(ctx, askuser.InsertParams{
		Token: uuid.NewString(), ConversationID: conversationID, Kind: "approval",
		Question: "Review before sending", Priority: 90, ToolCallID: "call-restart",
		ResumeContext: json.RawMessage(`{"type":"message_draft","draft_id":"` + draft.ID + `"}`),
	}); err != nil {
		t.Fatalf("persist review pause: %v", err)
	}
	if _, err := first.messageDrafts.ClaimSend(ctx, localIdentityID, draft.ID, nil); err != nil {
		t.Fatalf("first runner claims the send: %v", err)
	}
	asOwner(t, pool, localIdentityID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE aura.message_drafts SET dispatch_started_at=now()-interval '31 minutes' WHERE id=$1`, draft.ID)
		return err
	})

	restarted, _, _ := newIntegrationRunner(t, pool, agenttest.NewFakeClient())
	restarted.messageDrafts = messagedrafts.NewStore(pool)
	spy := &trustedSendSpy{}
	restarted.registry.Register(spy)
	if _, err := restarted.messageDrafts.Recover(ctx, localIdentityID); err != nil {
		t.Fatalf("recovery sweep: %v", err)
	}
	swept, err := restarted.messageDrafts.Get(ctx, localIdentityID, draft.ID)
	if err != nil || swept.Status != messagedrafts.StatusUncertain || swept.OutcomeCode != "interrupted" {
		t.Fatalf("after the sweep the draft is %s/%q (%v), want uncertain/interrupted", swept.Status, swept.OutcomeCode, err)
	}
	listed, err := restarted.ListMessageDrafts(ctx, conversationID)
	if err != nil || len(listed) != 1 || listed[0].ID != draft.ID {
		t.Fatalf("the unanswered review is not listed after the restart: %+v, error %v", listed, err)
	}
	resolution, err := restarted.ResolveMessageDraft(ctx, draft.ID, "send", nil)
	if err != nil || resolution.Status != messagedrafts.StatusUncertain {
		t.Fatalf("resolve after the restart: %+v, error %v", resolution, err)
	}
	if spy.executions != 0 {
		t.Fatalf("the send ran %d more times after the restart", spy.executions)
	}
	if countPersistedToolTurns(t, pool, conversationID) != 1 {
		t.Fatal("the review must persist exactly one tool answer")
	}
	var answer string
	asOwner(t, pool, localIdentityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT content FROM aura.conversation_turns WHERE conversation_id=$1 AND role='tool'`,
			conversationID).Scan(&answer)
	})
	if answer != "message delivery is uncertain; do not retry this call automatically" {
		t.Fatalf("answer = %q, want the do-not-retry instruction", answer)
	}
	if _, err := restarted.ResolveMessageDraft(ctx, draft.ID, "send", nil); !errors.Is(err, messagedrafts.ErrUnavailable) {
		t.Fatalf("a second resolve should not append another answer, got %v", err)
	}
}
