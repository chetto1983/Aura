package adaptive

import (
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	agenttools "github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

type recordingEventWriter struct {
	events []Event
}

func (r *recordingEventWriter) Record(_ context.Context, event Event) (int64, error) {
	r.events = append(r.events, event)
	return int64(len(r.events)), nil
}

func TestDecisionHookCapturesReasoningAndToolChoicesBeforeExecution(t *testing.T) {
	writer := &recordingEventWriter{}
	hook := NewDecisionHook(writer)
	owner := uuid.Must(uuid.NewV7())
	requestID := uuid.Must(uuid.NewV7())
	ctx := identityctx.WithIdentityID(context.Background(), owner.String())
	ctx = agenttools.WithRequestID(ctx, requestID.String())

	req := &llm.Request{
		SessionID:  "conversation-7",
		Model:      "qwen3.5-2b",
		ToolChoice: "auto",
		Reasoning:  llm.ReasoningConfig{Effort: llm.ReasoningEffortHigh},
		Messages:   []llm.Message{{Role: llm.RoleUser, Content: "private prompt"}},
		Tools:      []llm.ToolDef{toolDef("document_search")},
	}
	if result, err := hook.BeforeModel(ctx, req); err != nil || result != nil {
		t.Fatalf("BeforeModel = (%+v,%v), want (nil,nil)", result, err)
	}
	call := llm.ToolCall{ID: "tool-call-9"}
	call.Function.Name = "document_search"
	call.Function.Arguments = `{"query":"customer secret"}`
	if result, err := hook.BeforeTool(ctx, call); err != nil || result != nil {
		t.Fatalf("BeforeTool = (%+v,%v), want (nil,nil)", result, err)
	}

	if len(writer.events) != 2 {
		t.Fatalf("events = %d, want reasoning + tool decisions", len(writer.events))
	}
	for i, event := range writer.events {
		if event.OwnerID != owner ||
			event.AggregateID != requestID.String() || event.Kind != EventDecision {
			t.Fatalf("event[%d] correlation = %+v", i, event)
		}
		if string(event.Payload) == "" || containsSensitiveText(event.Payload, "private prompt", "customer secret") {
			t.Fatalf("event[%d] payload leaked request content: %s", i, event.Payload)
		}
	}
	wantReasoningDecision := DecisionIDForPoint(requestID, "reasoning")
	wantToolDecision := DecisionIDForPoint(requestID, "tool:tool-call-9")
	if writer.events[0].DecisionID != wantReasoningDecision ||
		writer.events[1].DecisionID != wantToolDecision ||
		writer.events[0].DecisionID == writer.events[1].DecisionID {
		t.Fatalf("assignment ids = reasoning:%s tool:%s, want distinct %s/%s",
			writer.events[0].DecisionID, writer.events[1].DecisionID,
			wantReasoningDecision, wantToolDecision)
	}

	firstIDs := []uuid.UUID{writer.events[0].ID, writer.events[1].ID}
	if _, err := hook.BeforeModel(ctx, req); err != nil {
		t.Fatal(err)
	}
	if _, err := hook.BeforeTool(ctx, call); err != nil {
		t.Fatal(err)
	}
	if writer.events[2].ID != firstIDs[0] || writer.events[3].ID != firstIDs[1] {
		t.Fatalf("retry event IDs changed: first=%v retry=%v", firstIDs, []uuid.UUID{writer.events[2].ID, writer.events[3].ID})
	}

	var _ agent.Hook = hook
}

func TestGatedDecisionHookRecordsOnlyInFreshObserveMode(t *testing.T) {
	writer := &recordingEventWriter{}
	reader := &mutablePolicyReader{policy: Policy{
		Epoch: 1, Version: "off", Mode: PolicyOff,
	}}
	hook := NewGatedDecisionHook(writer, NewPolicyController(reader))
	owner := uuid.Must(uuid.NewV7())
	requestID := uuid.Must(uuid.NewV7())
	ctx := identityctx.WithIdentityID(context.Background(), owner.String())
	ctx = agenttools.WithRequestID(ctx, requestID.String())
	req := &llm.Request{Model: "real-model"}

	if _, err := hook.BeforeModel(ctx, req); err != nil {
		t.Fatal(err)
	}
	if len(writer.events) != 0 {
		t.Fatalf("off-mode events = %d, want 0", len(writer.events))
	}
	reader.policy = Policy{Epoch: 2, Version: "shadow", Mode: PolicyShadow}
	if _, err := hook.BeforeModel(ctx, req); err != nil {
		t.Fatal(err)
	}
	if len(writer.events) != 1 {
		t.Fatalf("shadow-mode events = %d, want 1", len(writer.events))
	}
	reader.policy = Policy{Epoch: 3, Version: "rollback", Mode: PolicyRollback}
	if _, err := hook.BeforeModel(ctx, req); err != nil {
		t.Fatal(err)
	}
	if len(writer.events) != 1 {
		t.Fatalf("rollback-mode events = %d, want still 1", len(writer.events))
	}
}

func toolDef(name string) llm.ToolDef {
	var definition llm.ToolDef
	definition.Type = "function"
	definition.Function.Name = name
	return definition
}

func containsSensitiveText(payload []byte, values ...string) bool {
	text := string(payload)
	for _, value := range values {
		if value != "" && strings.Contains(text, value) {
			return true
		}
	}
	return false
}
