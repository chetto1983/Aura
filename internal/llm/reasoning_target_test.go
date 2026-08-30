package llm_test

import (
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// TestReasoningTarget locks the provider-neutral backend classifier: OpenRouter is
// recognized exactly as the historical prompt.IsOpenRouterReasoningTarget gate did,
// llama.cpp is keyed on an EXPLICIT provider=="llamacpp" (OQ-1, never a baseURL
// heuristic), and everything else — notably a local vLLM/DGX endpoint that also
// emits reasoning — classifies as None so the llama.cpp wire branch cannot misfire.
func TestReasoningTarget(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		baseURL  string
		want     llm.ReasoningTargetKind
	}{
		{"openrouter empty baseURL", "openrouter", "", llm.ReasoningTargetOpenRouter},
		{"openrouter canonical baseURL", "openrouter", "https://openrouter.ai/api/v1", llm.ReasoningTargetOpenRouter},
		{"openrouter case-insensitive", "OpenRouter", "", llm.ReasoningTargetOpenRouter},
		{"openrouter non-matching proxy baseURL", "openrouter", "https://custom.proxy/v1", llm.ReasoningTargetNone},
		{"llamacpp any baseURL", "llamacpp", "http://localhost:8080/v1", llm.ReasoningTargetLlamaCpp},
		{"llamacpp empty baseURL", "llamacpp", "", llm.ReasoningTargetLlamaCpp},
		{"llamacpp case-insensitive", "LLAMACPP", "http://localhost:8080", llm.ReasoningTargetLlamaCpp},
		{"ollama explicit provider", "ollama", "http://localhost:11434/v1", llm.ReasoningTargetOllama},
		{"ollama case-insensitive", "Ollama", "http://localhost:11434/v1", llm.ReasoningTargetOllama},
		{"vllm dgx local path is not llamacpp", "vllm", "http://dgx:8000", llm.ReasoningTargetNone},
		{"anthropic is none", "anthropic", "", llm.ReasoningTargetNone},
		{"empty provider is none", "", "", llm.ReasoningTargetNone},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := llm.ReasoningTarget(tc.provider, tc.baseURL); got != tc.want {
				t.Errorf("ReasoningTarget(%q, %q) = %v, want %v", tc.provider, tc.baseURL, got, tc.want)
			}
		})
	}
}

// TestReasoningEffortMax pins the net-new `max` vocabulary const to the OpenRouter
// wire token so an "extra/max" UI level serializes 1:1 (D-02/D-03/D-09a).
func TestReasoningEffortMax(t *testing.T) {
	if got := string(llm.ReasoningEffortMax); got != "max" {
		t.Errorf("string(ReasoningEffortMax) = %q, want %q", got, "max")
	}
}
