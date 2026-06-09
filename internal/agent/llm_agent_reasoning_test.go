package agent_test

import (
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

func TestLlmAgent_AdaptiveReasoningRouterKeepsToolsVisible(t *testing.T) {
	recordingProvider(t)
	fc := agenttest.NewFakeClient(
		agenttest.TextChunks("stop", `{"tier":"low"}`),
		agenttest.TextChunks("stop", "meteo pronto"),
	)
	a := agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:     fc,
		LLM:        llm.Config{Model: "m", Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1", TotalTimeoutSec: 30, AdaptiveReasoning: true, MaxTokens: 4096},
		Registry:   testRegistry(),
		PreviewCap: 2048,
		RunDir:     t.TempDir(),
		SessionID:  uuid.Must(uuid.NewV7()).String(),
		UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: "che tempo fa domani a Caraglio?"}},
	})

	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{MaxSteps: ptr(25)})))
	if err != nil {
		t.Fatalf("Run errored: %v", err)
	}
	if got := evs[len(evs)-1].LLMResponse.Content; got != "meteo pronto" {
		t.Fatalf("final content = %q", got)
	}
	if fc.CallCount() != 2 {
		t.Fatalf("client calls = %d, want router + main", fc.CallCount())
	}
	router := fc.Requests[0]
	if router.ToolChoice != "none" || len(router.Tools) != 0 {
		t.Fatalf("router request must be tool-free, got ToolChoice=%q tools=%d", router.ToolChoice, len(router.Tools))
	}
	if router.Reasoning.Enabled == nil || *router.Reasoning.Enabled {
		t.Fatalf("router request must disable reasoning, got %+v", router.Reasoning)
	}
	main := fc.Requests[1]
	if main.ToolChoice == "none" {
		t.Fatal("main request must not force ToolChoice=none")
	}
	if len(main.Tools) == 0 {
		t.Fatal("main request must keep tools/deferred discovery visible")
	}
	if main.Reasoning.Effort != llm.ReasoningEffortLow {
		t.Fatalf("main Reasoning.Effort = %q, want low", main.Reasoning.Effort)
	}
	if main.Reasoning.Exclude == nil || !*main.Reasoning.Exclude {
		t.Fatalf("main Reasoning.Exclude = %v, want true", main.Reasoning.Exclude)
	}
}

func TestLlmAgent_AdaptiveReasoningNoneDoesNotDisableTools(t *testing.T) {
	recordingProvider(t)
	fc := agenttest.NewFakeClient(
		agenttest.TextChunks("stop", `{"tier":"none"}`),
		agenttest.TextChunks("stop", "ciao"),
	)
	a := agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:     fc,
		LLM:        llm.Config{Model: "m", Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1", TotalTimeoutSec: 30, AdaptiveReasoning: true, MaxTokens: 4096},
		Registry:   testRegistry(),
		PreviewCap: 2048,
		RunDir:     t.TempDir(),
		SessionID:  uuid.Must(uuid.NewV7()).String(),
		UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: "ciao"}},
	})

	if _, err := collect(a.Run(newIC(t, agent.BudgetOptions{MaxSteps: ptr(25)}))); err != nil {
		t.Fatalf("Run errored: %v", err)
	}
	main := fc.Requests[1]
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
