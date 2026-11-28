package main

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/llm"
)

// The REPL footer follows the persisted cost: a Claude turn no call priced reads n/a, not a
// figure the rate table made up.
func TestCostFooterLeavesAnUnpricedClaudeTurnUnknown(t *testing.T) {
	cfg := &config.Config{LLM: llm.Config{
		Provider: llm.AnthropicProvider, Model: "claude-sonnet-5-5",
		Prices: map[string]llm.Price{"claude-sonnet-5-5": {InputPer1M: 2, OutputPer1M: 10}},
	}}
	footer := costFooterFromFinish(cfg, llm.Usage{PromptTokens: 1_000_000, CompletionTokens: 1_000_000}, "stop")
	if !strings.Contains(footer, "· n/a ·") {
		t.Fatalf("footer = %q, want n/a for an unpriced Claude turn", footer)
	}
	cfg.LLM.Provider = "openrouter"
	if footer := costFooterFromFinish(cfg, llm.Usage{PromptTokens: 1_000_000, CompletionTokens: 1_000_000}, "stop"); !strings.Contains(footer, "$12.000000") {
		t.Fatalf("footer = %q, want the table's $12 on another route", footer)
	}
}
