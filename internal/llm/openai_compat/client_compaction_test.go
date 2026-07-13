package openai_compat

import (
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestCapabilityEnvelopeRows(t *testing.T) {
	for _, provider := range []string{"openrouter", "llamacpp", "vllm"} {
		c := New(llm.Config{Provider: provider, BaseURL: "http://example", ContextWindow: 100_000, MaxOutputTokens: 4096})
		wire := c.buildWireRequest(llm.Request{Model: "m", Messages: []llm.Message{{Role: llm.RoleTool, ToolCallID: "c", Content: "ok"}}})
		if wire.Provider.DataCollection != "deny" || len(wire.Messages) != 1 || wire.Messages[0].ToolCallID != "c" {
			t.Fatalf("%s: %+v", provider, wire)
		}
	}
}
