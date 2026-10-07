package agent_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

type failingReasoningEmbedder struct{}

func (failingReasoningEmbedder) Embed(context.Context, []string) ([][]float64, error) {
	return nil, errors.New("embedding sidecar unavailable")
}

// uniformReasoningEmbedder embeds every text as the same vector and counts its calls. Every
// seed tier then scores alike, and the classifier's verdict is the first tier in label
// order, high, at margin 0.
type uniformReasoningEmbedder struct{ calls atomic.Int64 }

func (e *uniformReasoningEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	e.calls.Add(1)
	vecs := make([][]float64, len(texts))
	for i := range vecs {
		vecs[i] = []float64{1, 0, 0}
	}
	return vecs, nil
}

func adaptiveAgent(t *testing.T, fc llm.Client, user string, embedder prompt.Embedder, reading agent.TurnReading) *agent.LlmAgent {
	t.Helper()
	return agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:      fc,
		LLM:         llm.Config{Model: "m", Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1", TotalTimeoutSec: 30, AdaptiveReasoning: true, MaxTokens: 4096},
		Registry:    testRegistry(),
		PreviewCap:  2048,
		RunDir:      t.TempDir(),
		SessionID:   uuid.Must(uuid.NewV7()).String(),
		UserTurns:   []llm.Message{{Role: llm.RoleUser, Content: user}},
		Embedder:    embedder,
		TurnReading: reading,
	})
}

// With no classifier answer the turn is decided static low at once: the teacher is not
// waited for (spec amendment 2026-10-07), so the main request is the only one, and it keeps
// its tools.
func TestLlmAgent_AdaptiveReasoningFallsBackToStaticLowAndKeepsTools(t *testing.T) {
	for _, test := range []struct {
		name     string
		embedder prompt.Embedder
	}{
		{name: "no classifier"},
		{name: "the embedding fails", embedder: failingReasoningEmbedder{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			recordingProvider(t)
			fc := agenttest.NewFakeClient(agenttest.TextChunks("stop", "meteo pronto"))
			a := adaptiveAgent(t, fc, "che tempo fa domani a Caraglio?", test.embedder, agent.TurnReading{})

			evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{MaxSteps: new(25)})))
			if err != nil {
				t.Fatalf("Run errored: %v", err)
			}
			if got := evs[len(evs)-1].LLMResponse.Content; got != "meteo pronto" {
				t.Fatalf("final content = %q", got)
			}
			if fc.CallCount() != 1 {
				t.Fatalf("client calls = %d, want the main request only", fc.CallCount())
			}
			main := fc.Requests[0]
			if main.ToolChoice == "none" || len(main.Tools) == 0 {
				t.Fatalf("main request must keep tools visible; got ToolChoice=%q tools=%d", main.ToolChoice, len(main.Tools))
			}
			if main.Reasoning.Effort != llm.ReasoningEffortLow {
				t.Fatalf("main Reasoning.Effort = %q, want static low", main.Reasoning.Effort)
			}
			if main.Reasoning.Exclude == nil || !*main.Reasoning.Exclude {
				t.Fatalf("main Reasoning.Exclude = %v, want true", main.Reasoning.Exclude)
			}
		})
	}
}

// A standalone greeting is decided none; none must not disable tools.
func TestLlmAgent_AdaptiveReasoningNoneDoesNotDisableTools(t *testing.T) {
	recordingProvider(t)
	fc := agenttest.NewFakeClient(agenttest.TextChunks("stop", "ciao"))
	a := adaptiveAgent(t, fc, "ciao", nil, agent.TurnReading{Standalone: true})

	if _, err := collect(a.Run(newIC(t, agent.BudgetOptions{MaxSteps: new(25)}))); err != nil {
		t.Fatalf("Run errored: %v", err)
	}
	if fc.CallCount() != 1 {
		t.Fatalf("client calls = %d, want the main request only", fc.CallCount())
	}
	main := fc.Requests[0]
	if main.ToolChoice == "none" {
		t.Fatal("none reasoning tier must not disable tools")
	}
	if len(main.Tools) == 0 {
		t.Fatal("none reasoning tier must keep deferred tools discoverable")
	}
	if main.Reasoning.Effort != llm.ReasoningEffortNone {
		t.Fatalf("main Reasoning.Effort = %q, want none", main.Reasoning.Effort)
	}
	if main.Reasoning.Exclude == nil || !*main.Reasoning.Exclude {
		t.Fatalf("main Reasoning.Exclude = %v, want true", main.Reasoning.Exclude)
	}
}
