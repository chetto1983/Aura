package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/gateway"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/messagedrafts"
	"github.com/jackc/pgx/v5/pgconn"
)

type draftSpyTool struct {
	spec  tools.Spec
	calls int
	args  json.RawMessage
}

func (s *draftSpyTool) Spec() tools.Spec { return s.spec }
func (s *draftSpyTool) Execute(_ context.Context, args json.RawMessage) (tools.ToolResult, error) {
	s.calls++
	s.args = args
	return tools.ToolResult{}, nil
}

type draftSpyCreator struct {
	inputs []messagedrafts.DraftInput
	err    error
}

func (s *draftSpyCreator) Create(_ context.Context, input messagedrafts.DraftInput) (messagedrafts.Draft, error) {
	s.inputs = append(s.inputs, input)
	if s.err != nil {
		return messagedrafts.Draft{}, s.err
	}
	return messagedrafts.Draft{ID: "11111111-1111-4111-8111-111111111111"}, nil
}

func TestMessageDraftTrustedCallsParkBeforeExecute(t *testing.T) {
	cases := []struct {
		name, source, args string
		want               messagedrafts.Target
	}{
		{"pim__calendar", "recipe:calendar", `{"action":"send_email","to":["a@example.test"],"subject":"Hi","body":"Secret body"}`, messagedrafts.Target{Recipe: "recipe:calendar", Tool: "calendar", Action: "send_email"}},
		{"wa__send_message", "recipe:whatsapp", `{"recipient":"12345","message":"Secret body"}`, messagedrafts.Target{Recipe: "recipe:whatsapp", Tool: "send_message"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			registry := tools.NewRegistry()
			tool := &draftSpyTool{spec: tools.Spec{Name: tc.name, TrustedRecipeSource: tc.source, TrustedRecipeTool: tc.want.Tool, Mutating: true, Destructive: true}}
			registry.Register(tool)
			creator := &draftSpyCreator{}
			agent := newBareAgent(t, registry)
			agent.messageDrafts = creator
			owner := "11111111-1111-4111-8111-111111111111"
			ctx := gateway.WithResponder(identityctx.WithIdentityID(context.Background(), owner))
			pauses := agent.pauseCalls(ctx, []llm.ToolCall{toolCall("call-one", tc.name, tc.args)})
			if len(pauses) != 1 || len(creator.inputs) != 1 || tool.calls != 0 {
				t.Fatal("trusted outbound call did not park before execution")
			}
			input := creator.inputs[0]
			if input.IdentityID != owner || input.ToolCallID != "call-one" || input.Target != tc.want || input.RegisteredToolName != tc.name || !input.ExpiresAt.After(time.Now()) {
				t.Fatal("parked draft lost trusted call identity")
			}
			ic := internalPauseIC(t).WithContext(ctx)
			event := agent.pauseEvent(ic, [8]byte{}, nil, pauses[0].pause)
			event.Actions.AwaitingInput.OriginalArguments = pauses[0].originalArguments
			encoded, err := json.Marshal(event)
			if err != nil || string(encoded) == "" || bytes.Contains(encoded, []byte("Secret body")) {
				t.Fatal("draft event exposed message content")
			}
		})
	}
}

func TestMessageDraftSendFailsClosedWithoutReview(t *testing.T) {
	tool := &draftSpyTool{spec: tools.Spec{Name: "pim__calendar", TrustedRecipeSource: "recipe:calendar", TrustedRecipeTool: "calendar", Mutating: true}}
	agent := &LlmAgent{}
	_, err := agent.execTool(context.Background(), tool, true, json.RawMessage(`{"action":"send_email","to":["a@example.test"],"subject":"Hi"}`))
	if err == nil || tool.calls != 0 {
		t.Fatal("unreviewed email reached transport")
	}
	tool.spec.TrustedRecipeSource = ""
	if _, outbound := outboundMessageTarget(tool.Spec(), json.RawMessage(`{"action":"send_email"}`)); outbound {
		t.Fatal("untrusted server impersonated a managed send")
	}
}

func TestMessageDraftHashedRegisteredNameStillMatches(t *testing.T) {
	spec := tools.Spec{
		Name:                "namespace_that_is_very_long_and_needs_truncate__c_0123456789ab",
		TrustedRecipeSource: "recipe:calendar", TrustedRecipeTool: "calendar",
	}
	target, ok := outboundMessageTarget(spec, json.RawMessage(`{"action":"send_email"}`))
	if !ok || target.Tool != "calendar" {
		t.Fatal("hash-suffixed registered name lost the trusted raw tool identity")
	}
}

func TestExecuteReviewedMessageDispatchesApprovedEdits(t *testing.T) {
	target := messagedrafts.Target{Recipe: "recipe:whatsapp", Tool: "send_message"}
	original := json.RawMessage(`{"recipient":"12345","message":"Original"}`)
	effective, err := messagedrafts.MergeApprovedArgs(target, original, json.RawMessage(`{"recipient":"67890","message":"Edited"}`))
	if err != nil {
		t.Fatal(err)
	}
	bound, err := json.Marshal(struct {
		Recipe string          `json:"recipe"`
		Tool   string          `json:"tool"`
		Action string          `json:"action"`
		Args   json.RawMessage `json:"args"`
	}{Recipe: target.Recipe, Tool: target.Tool, Args: effective})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(bound)
	owner := "11111111-1111-4111-8111-111111111111"
	claim := messagedrafts.Draft{
		ID: "22222222-2222-4222-8222-222222222222", IdentityID: owner,
		ConversationID: "thread-one", ToolCallID: "call-one", Target: target,
		RegisteredToolName: "wa__send_message", Status: messagedrafts.StatusDispatching,
		EffectiveArgs: effective, EffectiveFingerprint: hex.EncodeToString(sum[:]),
	}
	tool := &draftSpyTool{spec: tools.Spec{Name: claim.RegisteredToolName, TrustedRecipeSource: target.Recipe, TrustedRecipeTool: target.Tool, Mutating: true}}
	if _, err := ExecuteReviewedMessage(identityctx.WithIdentityID(t.Context(), owner), tool, nil, claim, "", 0); err != nil {
		t.Fatal(err)
	}
	if tool.calls != 1 || !bytes.Equal(tool.args, effective) {
		t.Fatal("reviewed dispatch did not execute the exact edited arguments once")
	}
}

func TestMessageDraftErrorLogOmitsMessageContent(t *testing.T) {
	private := "private recipient and body"
	if got := safeMessageDraftError(errors.New(private)); bytes.Contains([]byte(got), []byte(private)) {
		t.Fatal("draft error log exposed message content")
	}
	if got := safeMessageDraftError(&pgconn.PgError{Code: "23514", Detail: private}); got != "postgres SQLSTATE 23514" {
		t.Fatal("draft error log did not retain the safe SQLSTATE")
	}
}

func TestMessageDraftCreateFailureIsLoggedWithoutContent(t *testing.T) {
	var log bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&log, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	registry := tools.NewRegistry()
	registry.Register(&draftSpyTool{spec: tools.Spec{
		Name: "pim__calendar", TrustedRecipeSource: "recipe:calendar", TrustedRecipeTool: "calendar",
	}})
	creator := &draftSpyCreator{err: &pgconn.PgError{Code: "23514", Detail: "private recipient and body"}}
	agent := newBareAgent(t, registry)
	agent.messageDrafts = creator
	ctx := gateway.WithResponder(identityctx.WithIdentityID(t.Context(), "11111111-1111-4111-8111-111111111111"))
	_, parked := agent.withholdOutboundMessage(ctx, toolCall("call-one", "pim__calendar",
		`{"action":"send_email","to":["private@example.test"],"subject":"Hello","body":"private body"}`))
	if parked || len(creator.inputs) != 1 {
		t.Fatal("failed draft write was parked as an actionable message")
	}
	if !bytes.Contains(log.Bytes(), []byte("postgres SQLSTATE 23514")) ||
		bytes.Contains(log.Bytes(), []byte("private body")) ||
		bytes.Contains(log.Bytes(), []byte("private@example.test")) ||
		bytes.Contains(log.Bytes(), []byte("private recipient and body")) {
		t.Fatal("draft write failure log lost its safe category or exposed message content")
	}
}
