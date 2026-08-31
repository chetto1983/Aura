package prompt

import (
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// The oracle classifies the CONVERSATION, not the backend, and every recognized backend
// already had a translation for the ReasoningConfig it produces: OpenRouter takes
// reasoning{effort}, Ollama takes reasoning_effort, and llamaCppReasoning maps every
// effort symbol onto thinking_budget_tokens / enable_thinking. The adaptive path was
// nevertheless gated on OpenRouter alone, so on every other backend the tier was computed
// and thrown away.
//
// What that cost, measured live on Ollama 0.33.2 with gemma4:31b-cloud, 2026-08-31:
// req.Reasoning stayed empty, no reasoning_effort was sent, and the model's default is not
// to think — adaptive reasoning was silently OFF for the life of the deployment. The same
// call WITH reasoning_effort comes back carrying a `reasoning` field. A manual selection
// from the composer reached Ollama the whole time (ApplyFixedReasoning gates on the
// generalized predicate), so the automatic path was the only one restricted.
func TestAdaptiveReasoningReachesEveryRecognizedBackend(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		provider string
		baseURL  string
	}{
		{"openrouter", "openrouter", "https://openrouter.ai/api/v1"},
		{"ollama", "ollama", "http://127.0.0.1:11434/v1"},
		{"llamacpp", "llamacpp", "http://127.0.0.1:8080/v1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			cfg := llm.Config{Provider: test.provider, BaseURL: test.baseURL, AdaptiveReasoning: true}
			req := &llm.Request{}
			ApplyAdaptiveReasoning(req, cfg.Provider, cfg, ReasoningTierHigh)
			if req.Reasoning.Effort != llm.ReasoningEffortHigh {
				t.Fatalf("effort = %q, want %q — the tier the oracle chose must reach this backend",
					req.Reasoning.Effort, llm.ReasoningEffortHigh)
			}
		})
	}
}

// An unrecognized backend still gets nothing: there is no translation for it, so sending a
// reasoning field would be inventing a wire contract.
func TestAdaptiveReasoningStaysOffAnUnrecognizedBackend(t *testing.T) {
	t.Parallel()
	cfg := llm.Config{Provider: "vllm", BaseURL: "http://dgx:8000/v1", AdaptiveReasoning: true}
	req := &llm.Request{}
	ApplyAdaptiveReasoning(req, cfg.Provider, cfg, ReasoningTierHigh)
	if !req.Reasoning.Empty() {
		t.Fatalf("reasoning = %+v, want untouched on a backend with no translation", req.Reasoning)
	}
}

// The operator switch still wins over everything.
func TestAdaptiveReasoningRespectsTheOperatorSwitch(t *testing.T) {
	t.Parallel()
	cfg := llm.Config{Provider: "ollama", BaseURL: "http://127.0.0.1:11434/v1", AdaptiveReasoning: false}
	req := &llm.Request{}
	ApplyAdaptiveReasoning(req, cfg.Provider, cfg, ReasoningTierHigh)
	if !req.Reasoning.Empty() {
		t.Fatalf("reasoning = %+v, want untouched when AURA_LLM_ADAPTIVE_REASONING is off", req.Reasoning)
	}
}
