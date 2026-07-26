package handlers

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
)

type agentJobReasoningControlProbe struct {
	mu     sync.Mutex
	inputs []agent.ReasoningControlInput
}

func (probe *agentJobReasoningControlProbe) DecideAndDeliverReasoning(
	ctx context.Context,
	input agent.ReasoningControlInput,
	learned agent.ReasoningRequestBuilder,
	_ agent.StaticReasoningRequestBuilder,
) (agent.PreparedReasoningRequest, error) {
	probe.mu.Lock()
	probe.inputs = append(probe.inputs, input)
	probe.mu.Unlock()
	return learned(ctx, agent.ReasoningActionStatic)
}

func (probe *agentJobReasoningControlProbe) snapshot() []agent.ReasoningControlInput {
	probe.mu.Lock()
	defer probe.mu.Unlock()
	return slices.Clone(probe.inputs)
}

func TestAgentJobThreadsReasoningControlIntoRealModelRound(t *testing.T) {
	control := &agentJobReasoningControlProbe{}
	client := agenttest.NewFakeClient(agenttest.TextChunks("stop", "done"))
	llmConfig := llm.Config{
		Provider: "openrouter", Model: "test-model",
		MaxTokens: 256, TotalTimeoutSec: 30,
	}
	handler := AgentJobHandler{Deps: AgentDeps{
		Client: client, LLM: llmConfig, Registry: jobRegistry(),
		ReasoningControl: control,
	}}
	ctx := identityctx.WithIdentityID(
		context.Background(),
		identityctx.LocalOperatorIdentity,
	)

	summary, err := handler.Run(ctx, Job{
		Payload: []byte(`{"goal":"finish the scheduled report"}`),
		RunID:   "run-reasoning-control",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if summary != "done" {
		t.Fatalf("summary = %q, want done", summary)
	}

	inputs := control.snapshot()
	if len(inputs) != 1 {
		t.Fatalf("reasoning decisions = %d, want 1", len(inputs))
	}
	input := inputs[0]
	if input.OwnerID.String() != identityctx.LocalOperatorIdentity ||
		input.RequestID.Version() != 7 ||
		input.PointOrdinal != 1 ||
		input.MessageCount != 2 ||
		input.ProviderID != llmConfig.Provider ||
		input.ModelID != llmConfig.Model {
		t.Fatalf("agent_job reasoning input = %+v", input)
	}
}

var _ agent.ReasoningControl = (*agentJobReasoningControlProbe)(nil)
