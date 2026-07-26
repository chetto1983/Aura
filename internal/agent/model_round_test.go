package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

type modelRoundCaptureClient struct {
	mu      sync.Mutex
	turns   []modelRoundTurn
	next    int
	rounds  []modelRound
	present []bool
}

type modelRoundTurn struct {
	chunks []llm.Chunk
	err    error
}

func (c *modelRoundCaptureClient) Stream(ctx context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	round, ok := modelRoundFromContext(ctx)
	c.mu.Lock()
	c.rounds = append(c.rounds, round)
	c.present = append(c.present, ok)
	var turn modelRoundTurn
	if c.next < len(c.turns) {
		turn = c.turns[c.next]
	}
	c.next++
	c.mu.Unlock()
	if turn.err != nil {
		return nil, turn.err
	}
	ch := make(chan llm.Chunk, len(turn.chunks))
	for _, chunk := range turn.chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

func (c *modelRoundCaptureClient) snapshot() ([]modelRound, []bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]modelRound(nil), c.rounds...), append([]bool(nil), c.present...)
}

type modelRoundProbeTool struct{}

func (modelRoundProbeTool) Spec() tools.Spec {
	return tools.Spec{
		Name:        "model_round_probe",
		Summary:     "Exercise a non-terminal primary-model tool iteration.",
		Description: "Exercise a non-terminal primary-model tool iteration.",
		Parameters:  json.RawMessage(`{"type":"object"}`),
	}
}

func (modelRoundProbeTool) Execute(ctx context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	return tools.NewResult(ctx, "ok")
}

func TestPrimaryModelRoundIncrementsOnlyForNewRequest(t *testing.T) {
	requestID := uuid.Must(uuid.NewV7())
	client := &modelRoundCaptureClient{turns: []modelRoundTurn{
		modelRoundToolTurn(modelRoundToolCall("call-1", "model_round_probe", `{}`)),
		modelRoundTextTurn("stop", "done"),
	}}
	runModelRoundAgent(t, client, requestID, llm.Config{}, 3)

	rounds, present := client.snapshot()
	if len(rounds) != 2 || len(present) != 2 || !present[0] || !present[1] {
		t.Fatalf("captured rounds = %+v present=%v, want two primary rounds", rounds, present)
	}
	if rounds[0].requestID != requestID || rounds[1].requestID != requestID {
		t.Fatalf("round request IDs = %s, %s; want %s", rounds[0].requestID, rounds[1].requestID, requestID)
	}
	if rounds[0].ordinal != 1 || rounds[1].ordinal != 2 {
		t.Fatalf("round ordinals = %d, %d; want 1, 2", rounds[0].ordinal, rounds[1].ordinal)
	}
}

func TestPrimaryModelRoundRetriesReuseIdentity(t *testing.T) {
	tests := []struct {
		name  string
		turns []modelRoundTurn
	}{
		{
			name: "stream open retry",
			turns: []modelRoundTurn{
				{err: errors.New("connection reset by peer")},
				modelRoundTextTurn("stop", "recovered"),
			},
		},
		{
			name: "mid stream retry",
			turns: []modelRoundTurn{
				modelRoundTextErrorTurn(errors.New("connection reset by peer"), "discard"),
				modelRoundTextTurn("stop", "recovered"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requestID := uuid.Must(uuid.NewV7())
			client := &modelRoundCaptureClient{turns: tt.turns}
			runModelRoundAgent(t, client, requestID, llm.Config{}, 3)

			rounds, present := client.snapshot()
			if len(rounds) != 2 || !present[0] || !present[1] {
				t.Fatalf("captured rounds = %+v present=%v, want two transport attempts", rounds, present)
			}
			if rounds[0] != rounds[1] {
				t.Fatalf("transport retry changed model-round identity: first=%+v second=%+v", rounds[0], rounds[1])
			}
			if rounds[0].requestID != requestID || rounds[0].ordinal != 1 {
				t.Fatalf("retry identity = %+v, want request %s ordinal 1", rounds[0], requestID)
			}
		})
	}
}

func TestReasoningRouterDoesNotConsumePrimaryModelRound(t *testing.T) {
	requestID := uuid.Must(uuid.NewV7())
	client := &modelRoundCaptureClient{turns: []modelRoundTurn{
		modelRoundTextTurn("stop", "low"),
		modelRoundTextTurn("stop", "done"),
	}}
	runModelRoundAgent(t, client, requestID, llm.Config{
		AdaptiveReasoning: true,
		Provider:          "openrouter",
	}, 2)

	rounds, present := client.snapshot()
	if len(rounds) != 2 {
		t.Fatalf("calls = %d, want router plus primary", len(rounds))
	}
	if present[0] {
		t.Fatalf("reasoning router consumed primary round %+v", rounds[0])
	}
	if !present[1] || rounds[1].requestID != requestID || rounds[1].ordinal != 1 {
		t.Fatalf("primary round = %+v present=%v, want request %s ordinal 1", rounds[1], present[1], requestID)
	}
}

func modelRoundTextTurn(finish string, parts ...string) modelRoundTurn {
	chunks := make([]llm.Chunk, 0, len(parts)+1)
	for _, part := range parts {
		chunks = append(chunks, llm.Chunk{Text: part})
	}
	return modelRoundTurn{chunks: append(chunks, llm.Chunk{FinishReason: finish})}
}

func modelRoundTextErrorTurn(err error, parts ...string) modelRoundTurn {
	turn := modelRoundTextTurn("", parts...)
	turn.chunks = append(turn.chunks, llm.Chunk{Err: err})
	return turn
}

func modelRoundToolTurn(calls ...llm.ToolCall) modelRoundTurn {
	chunks := make([]llm.Chunk, 0, len(calls)+1)
	for i := range calls {
		call := calls[i]
		chunks = append(chunks, llm.Chunk{ToolCall: &call})
	}
	return modelRoundTurn{chunks: append(chunks, llm.Chunk{FinishReason: "tool_calls"})}
}

func modelRoundToolCall(id, name, arguments string) llm.ToolCall {
	var call llm.ToolCall
	call.ID = id
	call.Type = "function"
	call.Function.Name = name
	call.Function.Arguments = arguments
	return call
}

func runModelRoundAgent(
	t *testing.T,
	client llm.Client,
	requestID uuid.UUID,
	cfg llm.Config,
	maxSteps int,
) {
	t.Helper()
	if cfg.Model == "" {
		cfg.Model = "test-model"
	}
	if cfg.Provider == "" {
		cfg.Provider = "test-provider"
	}
	if cfg.TotalTimeoutSec == 0 {
		cfg.TotalTimeoutSec = 30
	}
	registry := tools.NewRegistry()
	registry.Register(modelRoundProbeTool{})
	budget, err := NewBudget(BudgetOptions{MaxSteps: &maxSteps})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	a := NewLlmAgent(LlmAgentConfig{
		Client:     client,
		LLM:        cfg,
		Registry:   registry,
		PreviewCap: 1024,
		RunDir:     t.TempDir(),
		SessionID:  uuid.Must(uuid.NewV7()).String(),
		UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: "run"}},
	})
	ic := InvocationContext{
		Ctx:       context.Background(),
		Agent:     a,
		RequestID: requestID,
		Branch:    "root",
		Budget:    budget,
	}
	for _, runErr := range a.Run(ic) {
		if runErr != nil {
			t.Fatalf("Run: %v", runErr)
		}
	}
}
