package telegram

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/messagedrafts"
)

// sendSpy is the mounted WhatsApp send, with the operation metadata every mutating MCP tool gets.
type sendSpy struct{ calls int }

func (s *sendSpy) Spec() tools.Spec {
	return tools.Spec{
		Name: "wa__send_message", TrustedRecipeSource: "recipe:whatsapp", TrustedRecipeTool: "send_message",
		Mutating: true, OperationScope: tools.OperationScopeMCP,
		OperationNormalizer: tools.OperationNormalizerCanonical, ReplayPolicy: tools.ReplayToolResult,
	}
}

func (s *sendSpy) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	s.calls++
	return tools.ToolResult{}, nil
}

// Decision 34: a callback carries no operation, and the reviewed send derives its own from the
// parent. The approval operation Telegram roots is one the send accepts, as the cockpit's HTTP
// operation is (agent.TestExecuteReviewedMessageDispatchesUnderTheResolveOperation).
func TestTelegramApprovaRootsAnOperationTheSendAccepts(t *testing.T) {
	target := messagedrafts.Target{Recipe: "recipe:whatsapp", Tool: "send_message"}
	effective, err := messagedrafts.MergeApprovedArgs(target, json.RawMessage(`{"recipient":"10000000000","message":"Ciao"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	bound, _ := json.Marshal(struct {
		Recipe string          `json:"recipe"`
		Tool   string          `json:"tool"`
		Action string          `json:"action"`
		Args   json.RawMessage `json:"args"`
	}{Recipe: target.Recipe, Tool: target.Tool, Args: effective})
	sum := sha256.Sum256(bound)
	claim := messagedrafts.Draft{
		ID: "22222222-2222-4222-8222-222222222222", IdentityID: "10000000-0000-4000-8000-000000000001",
		ConversationID: "conv", ToolCallID: "call-1", Target: target, RegisteredToolName: "wa__send_message",
		Status: messagedrafts.StatusDispatching, EffectiveArgs: effective, EffectiveFingerprint: hex.EncodeToString(sum[:]),
	}
	ctx, err := reviewOperation(ownerReviewCtx(), claim.ID)
	if err != nil {
		t.Fatalf("root the operation: %v", err)
	}
	spy := &sendSpy{}
	if _, err := agent.ExecuteReviewedMessage(ctx, spy, nil, claim, "", 0); err != nil || spy.calls != 1 {
		t.Fatalf("send under the Telegram operation = %v, %d calls", err, spy.calls)
	}
}
