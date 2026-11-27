//go:build db_integration

package runner

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/messagedrafts"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Measured 2026-11-27 in the cockpit: the operator rewrote the recipient and the text of a review,
// the edited mail went out, and the model read only {"success": true}. It went on believing its own
// recipient and text had been sent.
func TestAnEditedSendTellsTheModelWhatWentOut(t *testing.T) {
	pool := migratedRunnerPool(t)
	ctx := ownerCtx()
	r, convStore, pauseStore := newIntegrationRunner(t, pool, agenttest.NewFakeClient())
	r.messageDrafts = messagedrafts.NewStore(pool)
	spy := &emailSendSpy{}
	r.registry.Register(spy)
	conversationID := newIntegrationConversation(t, pool, convStore)

	draft, err := r.messageDrafts.Create(ctx, messagedrafts.DraftInput{
		IdentityID: localIdentityID, ConversationID: conversationID, ToolCallID: "call-edit",
		Target:             messagedrafts.Target{Recipe: "recipe:calendar", Tool: "send_email"},
		RegisteredToolName: mcptools.NamespacedName("calendar", "send_email"),
		OriginalArgs:       json.RawMessage(`{"accountId":"acc-1","to":["colleage@aura.test"],"subject":"Riunione","body":"alle 10","bodyFormat":"text"}`),
		ExpiresAt:          time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create review draft: %v", err)
	}
	if err := pauseStore.Insert(ctx, askuser.InsertParams{
		Token: uuid.NewString(), ConversationID: conversationID, Kind: "approval",
		Question: "Review before sending", Priority: 90, ToolCallID: "call-edit",
		ResumeContext: json.RawMessage(`{"type":"message_draft","draft_id":"` + draft.ID + `"}`),
	}); err != nil {
		t.Fatalf("persist review pause: %v", err)
	}

	edits := json.RawMessage(`{"to":["colleague@aura.test"],"subject":"Riunione","body":"alle 11"}`)
	if resolution, err := r.ResolveMessageDraft(ctx, draft.ID, "send", edits); err != nil || resolution.Status != messagedrafts.StatusSent {
		t.Fatalf("send the edited review: %+v, error %v", resolution, err)
	}
	if len(spy.sent) != 1 || !strings.Contains(string(spy.sent[0]), `"colleague@aura.test"`) {
		t.Fatalf("send_email received %q, want the edited message once", spy.sent)
	}
	var answer string
	asOwner(t, pool, localIdentityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT content FROM aura.conversation_turns WHERE conversation_id=$1 AND role='tool'`,
			conversationID).Scan(&answer)
	})
	want := "The operator edited your message before approving it. These fields differ from your call: " +
		`to = ["colleague@aura.test"]; body = "alle 11".` + "\n\nemail sent"
	if answer != want {
		t.Fatalf("answer = %q\nwant     %q", answer, want)
	}
}
