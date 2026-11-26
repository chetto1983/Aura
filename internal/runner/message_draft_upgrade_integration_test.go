//go:build db_integration

package runner

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/dbtest"
	"github.com/chetto1983/aura/internal/messagedrafts"
	"github.com/chetto1983/aura/internal/pimsplit"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// emailSendSpy is the split PIM's mounted send_email; it records what it is asked to send.
type emailSendSpy struct{ sent []json.RawMessage }

func (s *emailSendSpy) Spec() tools.Spec {
	return tools.Spec{
		Name:                mcptools.NamespacedName("calendar", "send_email"),
		TrustedRecipeSource: "recipe:calendar", TrustedRecipeTool: "send_email", Mutating: true,
	}
}

func (s *emailSendSpy) Execute(_ context.Context, args json.RawMessage) (tools.ToolResult, error) {
	s.sent = append(s.sent, args)
	return tools.ToolResult{Preview: "email sent"}, nil
}

// A review drafted by an Aura from before the PIM split names the multiplexed calendar tool. After
// the upgrade it must still send, once, through send_email. The pim split's own tests stop at the
// claim; on the lab VM the one rewritten draft had expired, so the send was never seen there either.
func TestAReviewDraftedBeforeThePIMSplitSendsAfterIt(t *testing.T) {
	pool := migratedRunnerPool(t)
	ctx := ownerCtx()
	r, convStore, pauseStore := newIntegrationRunner(t, pool, agenttest.NewFakeClient())
	r.messageDrafts = messagedrafts.NewStore(pool)
	spy := &emailSendSpy{}
	r.registry.Register(spy)
	conversationID := newIntegrationConversation(t, pool, convStore)

	migrate, err := pgx.Connect(ctx, dbtest.MigrateURL(t, envOrSkip(t, "AURA_DB_MIGRATE_URL")))
	if err != nil {
		t.Fatalf("connect as the migrate role: %v", err)
	}
	t.Cleanup(func() { _ = migrate.Close(context.Background()) })
	draftID := uuid.NewString()
	if _, err := migrate.Exec(ctx, `INSERT INTO aura.message_drafts
(id, identity_id, conversation_id, tool_call_id, recipe, tool_name, registered_tool_name, action,
 original_args, original_fingerprint, status, expires_at)
VALUES ($1, $2, $3, 'call-legacy', 'recipe:calendar', 'calendar', $4, 'send_email', $5, repeat('a', 64), 'pending', $6)`,
		draftID, localIdentityID, conversationID, mcptools.NamespacedName("calendar", "calendar"),
		`{"action":"send_email","accountId":"acc-1","to":["a@example.com"],"subject":"Hi","body":"Hello"}`,
		time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("store the pre-split draft: %v", err)
	}
	t.Cleanup(func() {
		_, _ = migrate.Exec(context.Background(), `DELETE FROM aura.message_drafts WHERE id = $1`, draftID)
	})
	// The upgrade runs once per database; re-arm it, as its own tests do, so it settles this row.
	if _, err := migrate.Exec(ctx, `DELETE FROM aura.data_upgrades WHERE name = 'pim_tool_split'`); err != nil {
		t.Fatalf("re-arm the upgrade: %v", err)
	}
	if report, err := pimsplit.Upgrade(ctx, migrate, slog.New(slog.DiscardHandler)); err != nil || report.DraftsRewritten < 1 {
		t.Fatalf("upgrade = %+v, %v; want the pre-split draft rewritten", report, err)
	}
	if err := pauseStore.Insert(ctx, askuser.InsertParams{
		Token: uuid.NewString(), ConversationID: conversationID, Kind: "approval",
		Question: "Review before sending", Priority: 90, ToolCallID: "call-legacy",
		ResumeContext: json.RawMessage(`{"type":"message_draft","draft_id":"` + draftID + `"}`),
	}); err != nil {
		t.Fatalf("persist review pause: %v", err)
	}

	resolution, err := r.ResolveMessageDraft(ctx, draftID, "send", nil)
	if err != nil || resolution.Status != messagedrafts.StatusSent {
		t.Fatalf("send the upgraded review: %+v, error %v", resolution, err)
	}
	if len(spy.sent) != 1 {
		t.Fatalf("send_email ran %d times, want once", len(spy.sent))
	}
	var sent map[string]any
	if err := json.Unmarshal(spy.sent[0], &sent); err != nil {
		t.Fatalf("sent arguments: %v", err)
	}
	if _, has := sent["action"]; has || sent["accountId"] != "acc-1" || sent["subject"] != "Hi" || sent["body"] != "Hello" {
		t.Fatalf("send_email received %s, want the pre-split message without its action", spy.sent[0])
	}
	var answer string
	asOwner(t, pool, localIdentityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT content FROM aura.conversation_turns WHERE conversation_id=$1 AND role='tool'`,
			conversationID).Scan(&answer)
	})
	if answer != "email sent" {
		t.Fatalf("answer = %q, want the send's own result", answer)
	}
	if _, err := r.ResolveMessageDraft(ctx, draftID, "send", nil); !errors.Is(err, messagedrafts.ErrUnavailable) || len(spy.sent) != 1 {
		t.Fatalf("a second send = %v after %d sends; want ErrUnavailable and no second send", err, len(spy.sent))
	}
}
