package openai_compat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// The no-wire-cap contract. A Request with no MaxTokens must reach the provider with NO
// max_tokens key — not with zero, which is a stricter cap than any number.
//
// The one caller that asks for this is the conversation summarizer. An output cap there cuts
// the summary mid-section (a reasoning model spends the cap on reasoning first), the stream
// ends finish_reason="length", Summarize fails, and the compaction that was meant to condense
// the history hard-drops it instead. Measured live on this deployment 2026-08-17 at a cap of
// 1024. hermes-agent guards the same invariant for the same reason
// (agent/context_compressor.py: "NEVER add a max_tokens wire cap on the summary call").
func TestWireOmitsMaxTokensWhenTheCallerAsksForTheModelsOwnCeiling(t *testing.T) {
	for _, provider := range []string{"openrouter", "llamacpp", "vllm"} {
		t.Run(provider, func(t *testing.T) {
			c := New(llm.Config{Provider: provider, BaseURL: "http://example", ContextWindow: 100_000, MaxOutputTokens: 4096})
			body, err := json.Marshal(c.buildWireRequest(llm.Request{
				Model:    "m",
				Messages: []llm.Message{{Role: llm.RoleUser, Content: "summarize this"}},
			}))
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if strings.Contains(string(body), "max_tokens") {
				t.Fatalf("max_tokens reached the wire with no cap requested: %s", body)
			}
		})
	}
}

// The other half of the contract: a caller that DOES set a cap still sends it, so every
// non-summarizer request is byte-unchanged.
func TestWireKeepsAnExplicitMaxTokens(t *testing.T) {
	c := New(llm.Config{Provider: "openrouter", BaseURL: "http://example", ContextWindow: 100_000, MaxOutputTokens: 4096})
	wire := c.buildWireRequest(llm.Request{Model: "m", MaxTokens: 4096})
	if wire.MaxTokens != 4096 {
		t.Fatalf("MaxTokens = %d, want the caller's 4096", wire.MaxTokens)
	}
	body, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(body), `"max_tokens":4096`) {
		t.Fatalf("an explicit cap must reach the wire: %s", body)
	}
}
