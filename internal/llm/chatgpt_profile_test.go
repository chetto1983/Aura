package llm_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestChatGPTProfileDoesNotInheritCloudPricesOrWindow(t *testing.T) {
	cfg := llm.Config{
		Provider: llm.ChatGPTProvider, Model: "account-model", BaseURL: llm.ChatGPTBaseURL,
		APIKey: "unrelated-openrouter-key", Headers: map[string]string{"Authorization": "old-key"},
		ContextWindow: 1_000_000, MaxTokens: 4096, MaxOutputTokens: 384_000, TotalTimeoutSec: 120,
		Prices: map[string]llm.Price{"account-model": {InputPer1M: 10}},
	}
	if err := cfg.ResolveModelProfile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if cfg.ContextWindow != 32768 || cfg.MaxOutputTokens != llm.DerivedMaxOutputTokens(cfg.ContextWindow) {
		t.Fatalf("inherited budget: window %d, output %d", cfg.ContextWindow, cfg.MaxOutputTokens)
	}
	if cfg.APIKey != "" || len(cfg.Headers) != 0 || cfg.CostStatus != llm.CostStatusSubscriptionIncluded {
		t.Fatal("ChatGPT profile retained another provider's authentication or pricing status")
	}
	if _, known := llm.CostUSDValue(cfg.Prices, cfg.Model, llm.Usage{PromptTokens: 100}); known {
		t.Fatal("subscription quota was represented as a per-token USD bill")
	}
}

func TestChatGPTProfilePreservesExplicitBudgetAndFailsAtomically(t *testing.T) {
	cfg := llm.Config{
		Provider: llm.ChatGPTProvider, Model: "account-model", ContextWindow: 128000,
		ContextWindowConfigured: true, MaxTokens: 4096, MaxOutputTokens: 16000,
		MaxOutputTokensConfigured: true, TotalTimeoutSec: 120,
	}
	if err := cfg.ResolveChatGPTProfile(); err != nil {
		t.Fatal(err)
	}
	if cfg.ContextWindow != 128000 || cfg.MaxOutputTokens != 16000 {
		t.Fatal("operator budget overwritten")
	}
	cfg.MaxTokens = 20000
	before := cfg
	if err := cfg.ResolveChatGPTProfile(); err == nil || !reflect.DeepEqual(cfg, before) {
		t.Fatal("invalid profile published or modified before validation")
	}
}
