package runner

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

type runnerReasoningControlProbe struct {
	mu     sync.Mutex
	inputs []agent.ReasoningControlInput
}

func (probe *runnerReasoningControlProbe) DecideAndDeliverReasoning(
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

func (probe *runnerReasoningControlProbe) snapshot() []agent.ReasoningControlInput {
	probe.mu.Lock()
	defer probe.mu.Unlock()
	return slices.Clone(probe.inputs)
}

func TestRunnerThreadsReasoningControlThroughRealMultiRoundTurn(t *testing.T) {
	probe := &runnerReasoningControlProbe{}
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(
			agenttest.MakeToolCall("call-time", "current_time", `{}`),
		),
		agenttest.TextChunks("stop", "done"),
	)
	registry := tools.NewRegistry()
	registry.Register(tools.CurrentTime{})
	r := newReasoningControlRunner(t, client, registry, probe)
	convID := newConvID(t)
	mustCreate(t, r, convID)

	if _, err := drain(r.Turn(
		context.Background(),
		convID,
		userPtr("read the time"),
	)); err != nil {
		t.Fatalf("Turn: %v", err)
	}

	inputs := probe.snapshot()
	if len(inputs) != 2 {
		t.Fatalf("reasoning decisions = %d, want 2", len(inputs))
	}
	ownerID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	for index, input := range inputs {
		if input.OwnerID != ownerID ||
			input.RequestID == uuid.Nil ||
			input.RequestID.Version() != 7 ||
			input.RequestID != inputs[0].RequestID ||
			input.PointOrdinal != uint32(index+1) {
			t.Fatalf("round %d identity = %+v", index, input)
		}
	}
	if inputs[0].MessageCount != 2 || inputs[1].MessageCount != 4 {
		t.Fatalf(
			"message counts = %d/%d, want 2/4",
			inputs[0].MessageCount,
			inputs[1].MessageCount,
		)
	}
}

func TestRunnerReinjectsReasoningControlForResumeAgent(t *testing.T) {
	probe := &runnerReasoningControlProbe{}
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(
			askUserCall("call-ask", "Proceed?", "approval"),
		),
		agenttest.TextChunks("stop", "resumed"),
	)
	registry := tools.NewRegistry()
	registry.Register(tools.AskUser{})
	r := newReasoningControlRunner(t, client, registry, probe)
	convID := newConvID(t)
	mustCreate(t, r, convID)

	if _, err := drain(r.Turn(
		context.Background(),
		convID,
		userPtr("run approval"),
	)); err != nil {
		t.Fatalf("initial Turn: %v", err)
	}
	pending, err := r.PendingFor(context.Background(), convID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending = %d, err=%v", len(pending), err)
	}
	if _, err := r.SubmitAnswer(
		context.Background(),
		pending[0].Token,
		ResponseInput{Action: askuser.ActionAccept, Content: "yes"},
	); err != nil {
		t.Fatalf("SubmitAnswer: %v", err)
	}
	if _, err := drain(r.Turn(
		context.Background(),
		convID,
		nil,
	)); err != nil {
		t.Fatalf("resume Turn: %v", err)
	}

	inputs := probe.snapshot()
	if len(inputs) != 2 ||
		inputs[0].RequestID == inputs[1].RequestID ||
		inputs[0].PointOrdinal != 1 ||
		inputs[1].PointOrdinal != 1 ||
		inputs[0].OwnerID != inputs[1].OwnerID {
		t.Fatalf("resume decisions = %+v", inputs)
	}
}

func newReasoningControlRunner(
	t *testing.T,
	client llm.Client,
	registry *tools.Registry,
	control agent.ReasoningControl,
) *Runner {
	t.Helper()
	return New(Deps{
		Conv:            newFakeConvStore(),
		Pause:           newFakePauseStore(),
		Identity:        newFakeIdentityStore(),
		CacheMetrics:    newFakeCacheMetricStore(),
		ToolInvocations: newFakeToolInvocationStore(),
		Client:          client,
		Registry:        registry,
		LLM: llm.Config{
			Model: "test-model", Provider: "openrouter",
			ContextWindow: 1_000_000, MaxOutputTokens: 32_768,
			MaxTokens: 256, TotalTimeoutSec: 30,
		},
		ReasoningControl: control,
		TitleTimeout:     time.Second,
		StopTimeout:      time.Second,
	})
}

var _ agent.ReasoningControl = (*runnerReasoningControlProbe)(nil)
