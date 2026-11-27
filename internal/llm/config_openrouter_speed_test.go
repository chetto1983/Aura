package llm_test

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestOpenRouterSpeedPreferencesFromEnv(t *testing.T) {
	load := func(t *testing.T, throughput, latency string) (*llm.Config, error) {
		t.Helper()
		isolateHome(t)
		clearLLMEnv(t)
		t.Setenv("OPENROUTER_API_KEY", "sk-test-speed")
		if throughput != "" {
			t.Setenv(llm.EnvOpenRouterPreferredMinThroughput, throughput)
		}
		if latency != "" {
			t.Setenv(llm.EnvOpenRouterPreferredMaxLatencySec, latency)
		}
		return llm.Load()
	}
	t.Run("set", func(t *testing.T) {
		cfg, err := load(t, "20", "10")
		if err != nil || cfg.OpenRouterPreferredMinThroughput != 20 || cfg.OpenRouterPreferredMaxLatencySec != 10 {
			t.Fatalf("cfg = %+v, err = %v, want 20 and 10", cfg, err)
		}
	})
	t.Run("unset sends nothing", func(t *testing.T) {
		cfg, err := load(t, "", "")
		if err != nil || cfg.OpenRouterPreferredMinThroughput != 0 || cfg.OpenRouterPreferredMaxLatencySec != 0 {
			t.Fatalf("cfg = %+v, err = %v, want zeros", cfg, err)
		}
	})
	t.Run("negative refused", func(t *testing.T) {
		_, err := load(t, "-1", "")
		if err == nil || !strings.Contains(err.Error(), llm.EnvOpenRouterPreferredMinThroughput) {
			t.Fatalf("err = %v, want a refusal naming %s", err, llm.EnvOpenRouterPreferredMinThroughput)
		}
	})
}
