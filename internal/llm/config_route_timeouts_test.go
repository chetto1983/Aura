package llm_test

import (
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/llm"
)

// Measured on the lab VM 2026-11-28: OpenRouter sent no byte for 78 s while a provider timed out,
// then its fallback answered in 5 s; a 60 s watchdog closed the stream first. Unset, the OpenRouter
// route gets room for one fallback; every other route keeps the shorter bounds.
func TestRouteTimeoutsFollowTheRoute(t *testing.T) {
	openRouter := llm.Config{Provider: "openrouter", BaseURL: llm.DefaultBaseURL}
	for _, tc := range []struct {
		name      string
		cfg       llm.Config
		idle, max time.Duration
	}{
		{"openrouter unset", openRouter, 90 * time.Second, 180 * time.Second},
		{"llamacpp unset", llm.Config{Provider: "llamacpp", BaseURL: "http://127.0.0.1:8080/v1"}, 60 * time.Second, 120 * time.Second},
		{"ollama unset", llm.Config{Provider: "ollama", BaseURL: "http://127.0.0.1:11434/v1"}, 60 * time.Second, 120 * time.Second},
		{"chatgpt unset", llm.Config{Provider: llm.ChatGPTProvider, BaseURL: llm.ChatGPTBaseURL}, 60 * time.Second, 120 * time.Second},
		{"anthropic unset", llm.Config{Provider: llm.AnthropicProvider, BaseURL: llm.AnthropicBaseURL}, 60 * time.Second, 120 * time.Second},
		{"openrouter explicit", llm.Config{Provider: "openrouter", BaseURL: llm.DefaultBaseURL,
			StreamIdleTimeoutSec: 30, TotalTimeoutSec: 100}, 30 * time.Second, 100 * time.Second},
		{"openrouter watchdog disabled", llm.Config{Provider: "openrouter", BaseURL: llm.DefaultBaseURL,
			StreamIdleTimeoutConfigured: true}, 0, 180 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.StreamIdleTimeout(); got != tc.idle {
				t.Errorf("StreamIdleTimeout() = %s, want %s", got, tc.idle)
			}
			if got := tc.cfg.CallTimeout(); got != tc.max {
				t.Errorf("CallTimeout() = %s, want %s", got, tc.max)
			}
		})
	}
}

func TestRouteTimeoutsFromEnv(t *testing.T) {
	load := func(t *testing.T, env map[string]string) *llm.Config {
		t.Helper()
		isolateHome(t)
		clearLLMEnv(t)
		t.Setenv("OPENROUTER_API_KEY", "sk-test-timeouts")
		for key, value := range env {
			t.Setenv(key, value)
		}
		cfg, err := llm.Load()
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	t.Run("unset follows the default OpenRouter route", func(t *testing.T) {
		cfg := load(t, nil)
		if cfg.StreamIdleTimeout() != 90*time.Second || cfg.CallTimeout() != 180*time.Second {
			t.Fatalf("timeouts = %s/%s, want 90s/180s", cfg.StreamIdleTimeout(), cfg.CallTimeout())
		}
	})
	t.Run("explicit values win", func(t *testing.T) {
		cfg := load(t, map[string]string{"AURA_LLM_STREAM_IDLE_TIMEOUT_SEC": "30", "AURA_LLM_TOTAL_TIMEOUT_SEC": "100"})
		if cfg.StreamIdleTimeout() != 30*time.Second || cfg.CallTimeout() != 100*time.Second {
			t.Fatalf("timeouts = %s/%s, want 30s/100s", cfg.StreamIdleTimeout(), cfg.CallTimeout())
		}
	})
	t.Run("an explicit 0 disables the watchdog", func(t *testing.T) {
		cfg := load(t, map[string]string{"AURA_LLM_STREAM_IDLE_TIMEOUT_SEC": "0"})
		if cfg.StreamIdleTimeout() != 0 {
			t.Fatalf("StreamIdleTimeout() = %s, want 0", cfg.StreamIdleTimeout())
		}
	})
}
