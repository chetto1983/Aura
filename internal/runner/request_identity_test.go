package runner

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

type requestIdentityClient struct {
	delegate *agenttest.FakeClient
	mu       sync.Mutex
	seen     []string
}

func (c *requestIdentityClient) Stream(ctx context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	c.mu.Lock()
	c.seen = append(c.seen, tools.RequestIDFromContext(ctx))
	c.mu.Unlock()
	return c.delegate.Stream(ctx, req)
}

type requestIdentityTool struct {
	seen string
}

func (t *requestIdentityTool) Spec() tools.Spec {
	return tools.Spec{
		Name:        "request_identity_probe",
		Summary:     "Report the current request identity.",
		Description: "Report the current request identity.",
		Parameters:  json.RawMessage(`{"type":"object"}`),
	}
}

func (t *requestIdentityTool) Execute(ctx context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	t.seen = tools.RequestIDFromContext(ctx)
	return tools.NewResult(ctx, t.seen)
}

func TestTurnMintsRequestIdentityBeforeContextAssemblyAndReusesItEverywhere(t *testing.T) {
	client := &requestIdentityClient{delegate: agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("call-1", "request_identity_probe", `{}`)),
		agenttest.TextChunks("stop", "done"),
	)}
	r, _, _ := newTestRunner(t, client)
	probe := &requestIdentityTool{}
	r.registry.Register(probe)
	convID := newConvID(t)
	mustCreate(t, r, convID)

	var recallRequestID string
	r.recallMaxItems = 8
	r.dynamicRecallProvider = func(
		ctx context.Context,
		_, _ string,
		_ int,
	) (DynamicRecall, error) {
		recallRequestID = tools.RequestIDFromContext(ctx)
		return validDynamicRecall(), nil
	}
	r.dynamicRecallControl = dynamicRecallControlFunc(func(
		ctx context.Context,
		input DynamicRecallInput,
		provider DynamicRecallProvider,
	) (*PreparedDynamicRecall, error) {
		recall, err := provider(ctx, input.OwnerID.String(), input.Query, 4)
		return &PreparedDynamicRecall{
			Action: DynamicRecallTop4,
			Recall: recall,
			Commit: func(context.Context, agent.DynamicTailOutcome) error {
				return nil
			},
		}, err
	})
	events, err := drain(r.Turn(context.Background(), convID, userPtr("inspect identity")))
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}

	requestID, err := uuid.Parse(recallRequestID)
	if err != nil || requestID.Version() != 7 {
		t.Fatalf("recall request ID = %q, want UUIDv7: %v", recallRequestID, err)
	}
	if probe.seen != recallRequestID {
		t.Fatalf("tool request ID = %q, recall saw %q", probe.seen, recallRequestID)
	}
	client.mu.Lock()
	modelIDs := append([]string(nil), client.seen...)
	client.mu.Unlock()
	if len(modelIDs) < 2 {
		t.Fatalf("model calls = %v, want two primary calls", modelIDs)
	}
	for i, got := range modelIDs[:2] {
		if got != recallRequestID {
			t.Fatalf("model call %d request ID = %q, want %q", i, got, recallRequestID)
		}
	}
	for i, event := range events {
		if event == nil {
			continue
		}
		if event.RequestID != requestID {
			t.Fatalf("event %d request ID = %s, want %s", i, event.RequestID, requestID)
		}
	}
	if len(events) == 0 {
		t.Fatal("turn emitted no events")
	}
}

var _ llm.Client = (*requestIdentityClient)(nil)
var _ tools.Tool = (*requestIdentityTool)(nil)
var _ agent.Agent = (*agent.LlmAgent)(nil)
