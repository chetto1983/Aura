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

func TestMessageDraftReviewDeclineCommitsOneAnswer(t *testing.T) {
	pool := migratedRunnerPool(t)
	r, convStore, pauseStore := newIntegrationRunner(t, pool, agenttest.NewFakeClient())
	r.messageDrafts = messagedrafts.NewStore(pool)
	conversationID := newIntegrationConversation(t, pool, convStore)
	ctx := ownerCtx()
	input := messagedrafts.DraftInput{
		IdentityID: localIdentityID, ConversationID: conversationID, ToolCallID: "call-review",
		Target:             messagedrafts.Target{Recipe: "recipe:whatsapp", Tool: "send_message"},
		RegisteredToolName: "whatsapp__send_message",
		OriginalArgs:       json.RawMessage(`{"recipient":"12345","message":"private text"}`),
		ExpiresAt:          time.Now().Add(time.Hour),
	}
	draft, err := r.messageDrafts.Create(ctx, input)
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

	// A persisted draft without its pause is never exposed as actionable.
	listed, err := r.ListMessageDrafts(ctx, conversationID)
	if err != nil || len(listed) != 0 {
		t.Fatalf("orphan review exposed: %d drafts, error %v", len(listed), err)
	}
	if err := pauseStore.Insert(ctx, askuser.InsertParams{
		Token: uuid.NewString(), ConversationID: conversationID, Kind: "approval",
		Question: "Review before sending", Priority: 90, ToolCallID: input.ToolCallID,
		ResumeContext: json.RawMessage(`{"type":"message_draft","draft_id":"` + draft.ID + `"}`),
	}); err != nil {
		t.Fatalf("persist review pause: %v", err)
	}
	listed, err = r.ListMessageDrafts(ctx, conversationID)
	if err != nil || len(listed) != 1 || listed[0].ID != draft.ID {
		t.Fatalf("review not joined to its pause: %+v, error %v", listed, err)
	}
	resolution, err := r.ResolveMessageDraft(ctx, draft.ID, "decline", nil)
	if err != nil || resolution.Status != messagedrafts.StatusDeclined {
		t.Fatalf("decline review: %+v, error %v", resolution, err)
	}
	if countPersistedToolTurns(t, pool, conversationID) != 1 {
		t.Fatal("decline must persist exactly one tool answer")
	}
	var answer string
	asOwner(t, pool, localIdentityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT content FROM aura.conversation_turns WHERE conversation_id=$1 AND role='tool'`,
			conversationID).Scan(&answer)
	})
	if answer != "user declined to send the message" {
		t.Fatalf("decline answer = %q, want no-send instruction", answer)
	}
	listed, err = r.ListMessageDrafts(ctx, conversationID)
	if err != nil || len(listed) != 0 {
		t.Fatalf("declined review remains actionable: %+v, error %v", listed, err)
	}
	if _, err := r.ResolveMessageDraft(ctx, draft.ID, "decline", nil); !errors.Is(err, messagedrafts.ErrUnavailable) {
		t.Fatalf("a second decline should not append another answer, got %v", err)
	}
}
