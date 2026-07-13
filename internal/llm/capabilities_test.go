package llm

import (
	"errors"
	"testing"
)

func TestCapabilityRegistry(t *testing.T) {
	for _, provider := range []string{"openrouter", "llamacpp", "vllm", "openai_compat"} {
		cap, err := CapabilityFor(Config{Provider: provider, Model: "test", ContextWindow: 128_000, MaxOutputTokens: 4096})
		if err != nil {
			t.Fatalf("%s: %v", provider, err)
		}
		if err := cap.ValidateForCompaction(); err != nil {
			t.Fatalf("%s invalid: %v", provider, err)
		}
		if cap.ContractVersion == "" || cap.Estimator.ID == "" || cap.Estimator.Version == "" {
			t.Fatalf("%s lacks versioned estimator: %+v", provider, cap)
		}
		if !cap.StructuredOutput.Schema || !cap.Usage.InputTokens || !cap.Usage.OutputTokens {
			t.Fatalf("%s lacks activation requirements", provider)
		}
		if cap.InternalContext.Authoritative {
			t.Fatalf("%s internal history must be non-authoritative", provider)
		}
		if cap.ToolOrdering != ToolOrderingCallThenResult {
			t.Fatalf("%s tool ordering = %q", provider, cap.ToolOrdering)
		}
	}
}

func TestCapabilityRegistryFailsClosed(t *testing.T) {
	for _, cfg := range []Config{{Provider: "unknown", ContextWindow: 1000, MaxOutputTokens: 1}, {Provider: "openrouter", ContextWindow: 0, MaxOutputTokens: 1}, {Provider: "openrouter", ContextWindow: 1000, MaxOutputTokens: -1}} {
		_, err := CapabilityFor(cfg)
		if !errors.Is(err, ErrCompactionCapabilityUnavailable) {
			t.Fatalf("cfg=%+v err=%v", cfg, err)
		}
	}
}

func TestFallbackUpperBound(t *testing.T) {
	if got := ConservativeTokenUpperBound(100); got != 371 {
		t.Fatalf("got %d", got)
	}
}
