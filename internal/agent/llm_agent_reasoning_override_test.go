package agent_test

import (
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// TestReasoningOverride proves the FIXED per-turn effort override (37E) forces
// req.Reasoning and BYPASSES the adaptive classifier on ANY reasoning target, while
// auto (empty override) leaves the adaptive path byte-identical (D-04 zero regression).
//
// The classifier-bypass is asserted structurally: with no embedder wired, the adaptive
// path makes a SEPARATE tool-free reasoning-router LLM call FIRST. A single client call
// (no router request) is proof the classifier was skipped; two calls (router + main)
// is proof it ran. The greeting user message would classify to a light tier under the
// adaptive path, so the override winning "high" also proves it ignores message content.
func TestReasoningOverride(t *testing.T) {
	t.Run("fixed_high_on_openrouter_bypasses_classifier", func(t *testing.T) {
		recordingProvider(t)
		fc := agenttest.NewFakeClient(
			agenttest.TextChunks("stop", "done"),
		)
		a := agent.NewLlmAgent(agent.LlmAgentConfig{
			Client:            fc,
			LLM:               llm.Config{Model: "m", Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1", TotalTimeoutSec: 30, AdaptiveReasoning: true, MaxTokens: 4096},
			Registry:          testRegistry(),
			PreviewCap:        2048,
			RunDir:            t.TempDir(),
			SessionID:         uuid.Must(uuid.NewV7()).String(),
			UserTurns:         []llm.Message{{Role: llm.RoleUser, Content: "ciao"}},
			ReasoningOverride: llm.ReasoningEffortHigh,
		})

		if _, err := collect(a.Run(newIC(t, agent.BudgetOptions{MaxSteps: new(25)}))); err != nil {
			t.Fatalf("Run errored: %v", err)
		}
		if fc.CallCount() != 1 {
			t.Fatalf("client calls = %d, want 1 (a fixed override must skip the reasoning router)", fc.CallCount())
		}
		main := fc.Requests[0]
		if main.ToolChoice == "none" || len(main.Tools) == 0 {
			t.Fatalf("fixed-override main request must keep tools visible; got ToolChoice=%q tools=%d", main.ToolChoice, len(main.Tools))
		}
		if main.Reasoning.Effort != llm.ReasoningEffortHigh {
			t.Fatalf("main Reasoning.Effort = %q, want high (forced regardless of the greeting content)", main.Reasoning.Effort)
		}
		if main.Reasoning.Exclude == nil || !*main.Reasoning.Exclude {
			t.Fatalf("main Reasoning.Exclude = %v, want true (D-10 exclude from ShowReasoning default)", main.Reasoning.Exclude)
		}
	})

	t.Run("fixed_high_on_llamacpp_fires", func(t *testing.T) {
		recordingProvider(t)
		fc := agenttest.NewFakeClient(
			agenttest.TextChunks("stop", "fatto"),
		)
		a := agent.NewLlmAgent(agent.LlmAgentConfig{
			Client:            fc,
			LLM:               llm.Config{Model: "m", Provider: "llamacpp", BaseURL: "http://127.0.0.1:8080/v1", TotalTimeoutSec: 30, AdaptiveReasoning: true, MaxTokens: 4096},
			Registry:          testRegistry(),
			PreviewCap:        2048,
			RunDir:            t.TempDir(),
			SessionID:         uuid.Must(uuid.NewV7()).String(),
			UserTurns:         []llm.Message{{Role: llm.RoleUser, Content: "ciao"}},
			ReasoningOverride: llm.ReasoningEffortHigh,
		})

		if _, err := collect(a.Run(newIC(t, agent.BudgetOptions{MaxSteps: new(25)}))); err != nil {
			t.Fatalf("Run errored: %v", err)
		}
		// The adaptive path is OpenRouter-only, so on llama.cpp WITHOUT the override the
		// request carries no reasoning at all. The fixed path must still fire here (D-08).
		if fc.CallCount() != 1 {
			t.Fatalf("client calls = %d, want 1 (no adaptive router on llama.cpp)", fc.CallCount())
		}
		main := fc.Requests[0]
		if main.Reasoning.Effort != llm.ReasoningEffortHigh {
			t.Fatalf("llama.cpp main Reasoning.Effort = %q, want high (fixed path must reach llama.cpp)", main.Reasoning.Effort)
		}
	})

	t.Run("auto_empty_override_runs_adaptive_unchanged", func(t *testing.T) {
		recordingProvider(t)
		// No override + no embedder => the tool-free reasoning router runs (turn 0), then
		// the main turn (turn 1) carries the router-selected tier — byte-identical to the
		// pre-37E adaptive path (mirrors TestLlmAgent_AdaptiveReasoningRouterKeepsToolsVisible).
		fc := agenttest.NewFakeClient(
			agenttest.TextChunks("stop", `{"tier":"high"}`),
			agenttest.TextChunks("stop", "risposta"),
		)
		a := agent.NewLlmAgent(agent.LlmAgentConfig{
			Client:     fc,
			LLM:        llm.Config{Model: "m", Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1", TotalTimeoutSec: 30, AdaptiveReasoning: true, MaxTokens: 4096},
			Registry:   testRegistry(),
			PreviewCap: 2048,
			RunDir:     t.TempDir(),
			SessionID:  uuid.Must(uuid.NewV7()).String(),
			UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: "debug this outage"}},
			// ReasoningOverride left zero => auto.
		})

		if _, err := collect(a.Run(newIC(t, agent.BudgetOptions{MaxSteps: new(25)}))); err != nil {
			t.Fatalf("Run errored: %v", err)
		}
		if fc.CallCount() != 2 {
			t.Fatalf("client calls = %d, want 2 (router + main); auto must still run the classifier", fc.CallCount())
		}
		router := fc.Requests[0]
		if router.ToolChoice != "none" || len(router.Tools) != 0 {
			t.Fatalf("auto path must first run the tool-free reasoning router; got ToolChoice=%q tools=%d", router.ToolChoice, len(router.Tools))
		}
		main := fc.Requests[1]
		if main.Reasoning.Effort != llm.ReasoningEffortHigh {
			t.Fatalf("auto main Reasoning.Effort = %q, want high (router tier applied unchanged)", main.Reasoning.Effort)
		}
	})
}
