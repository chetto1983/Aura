package llm_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestConfigValidateTokenBudget(t *testing.T) {
	valid := llm.Config{
		ContextWindow:   128_000,
		MaxTokens:       4096,
		MaxOutputTokens: 32_768,
	}
	cases := []struct {
		name   string
		mutate func(*llm.Config)
		field  string
	}{
		{"zero context window", func(cfg *llm.Config) { cfg.ContextWindow = 0 }, "context_window"},
		{"negative context window", func(cfg *llm.Config) { cfg.ContextWindow = -1 }, "context_window"},
		{"zero max tokens", func(cfg *llm.Config) { cfg.MaxTokens = 0 }, "max_tokens"},
		{"negative max tokens", func(cfg *llm.Config) { cfg.MaxTokens = -1 }, "max_tokens"},
		{"zero output reserve", func(cfg *llm.Config) { cfg.MaxOutputTokens = 0 }, "max_output_tokens"},
		{"negative output reserve", func(cfg *llm.Config) { cfg.MaxOutputTokens = -1 }, "max_output_tokens"},
		{"under reserved output", func(cfg *llm.Config) { cfg.MaxOutputTokens = cfg.MaxTokens - 1 }, "max_output_tokens"},
		// 33_000 used to be the no-budget case (33000 - 20000 - 13000 = 0). The reserves
		// scale with the window since 2026-08-16, so that window is valid now -- which is
		// what lets a 32k local model boot. Asking for 9000 output tokens out of 10000 is
		// still impossible, and that is the case worth pinning.
		{"no prompt budget", func(cfg *llm.Config) {
			cfg.ContextWindow = 10_000
			cfg.MaxTokens = 1
			cfg.MaxOutputTokens = 9_000
		}, "prompt budget"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := valid
			tc.mutate(&cfg)
			err := cfg.Validate()
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), tc.field) {
				t.Fatalf("Validate error = %v, want actionable %q error", err, tc.field)
			}
		})
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid token budget rejected: %v", err)
	}
}

func TestLoadRejectsInvalidTokenBudgetFromEnv(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
	}{
		{"zero context", map[string]string{"AURA_MODEL_CONTEXT_WINDOW": "0"}},
		{"negative max tokens", map[string]string{"AURA_LLM_MAX_TOKENS": "-1"}},
		{"zero output reserve", map[string]string{"AURA_MODEL_MAX_OUTPUT_TOKENS": "0"}},
		{"under reserved", map[string]string{
			"AURA_LLM_MAX_TOKENS": "4096", "AURA_MODEL_MAX_OUTPUT_TOKENS": "4095",
		}},
		{"no prompt budget", map[string]string{
			"AURA_LLM_MAX_TOKENS": "1", "AURA_MODEL_MAX_OUTPUT_TOKENS": "9000",
			"AURA_MODEL_CONTEXT_WINDOW": "10000",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isolateHome(t)
			clearLLMEnv(t)
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			if _, err := llm.LoadAllowEmptyKey(); err == nil {
				t.Fatal("LoadAllowEmptyKey accepted an invalid token budget")
			}
		})
	}
}

func TestLoadRejectsInvalidTokenBudgetFromFile(t *testing.T) {
	home := isolateHome(t)
	clearLLMEnv(t)
	auraDir := filepath.Join(home, ".aura")
	if err := os.MkdirAll(auraDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(auraDir, "llm.json")
	for name, payload := range map[string]string{
		"zero":           `{"context_window":0}`,
		"negative":       `{"max_tokens":-1}`,
		"under_reserved": `{"max_tokens":4096,"max_output_tokens":4095}`,
		// A 33000 window used to be the canonical no-budget case (33000 - 20000 - 13000 = 0).
		// Since 2026-08-16 the reserves scale with the window, so 33000 is valid and a
		// 32k-context local model boots. What is still impossible is asking for more output
		// than the window can hold: 9000 of output on a 10000 window leaves nothing to
		// prompt with.
		"no_prompt_budget": `{"max_tokens":1,"max_output_tokens":9000,"context_window":10000}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := llm.LoadAllowEmptyKey(); err == nil {
				t.Fatal("LoadAllowEmptyKey accepted an invalid llm.json token budget")
			}
		})
	}
}
