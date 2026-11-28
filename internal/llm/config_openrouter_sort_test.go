package llm_test

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestOpenRouterProviderSortFromEnv(t *testing.T) {
	load := func(t *testing.T, sort string) (*llm.Config, error) {
		t.Helper()
		isolateHome(t)
		clearLLMEnv(t)
		t.Setenv("OPENROUTER_API_KEY", "sk-test-sort")
		if sort != "" {
			t.Setenv(llm.EnvOpenRouterProviderSort, sort)
		}
		return llm.Load()
	}
	t.Run("set, trimmed and lower-cased", func(t *testing.T) {
		cfg, err := load(t, " Latency ")
		if err != nil || cfg.OpenRouterProviderSort != "latency" {
			t.Fatalf("cfg.OpenRouterProviderSort = %q, err = %v, want latency", cfg.OpenRouterProviderSort, err)
		}
	})
	t.Run("unset keeps OpenRouter's default routing", func(t *testing.T) {
		cfg, err := load(t, "")
		if err != nil || cfg.OpenRouterProviderSort != "" {
			t.Fatalf("cfg.OpenRouterProviderSort = %q, err = %v, want empty", cfg.OpenRouterProviderSort, err)
		}
	})
	t.Run("a value OpenRouter does not document is refused", func(t *testing.T) {
		_, err := load(t, "fastest")
		if err == nil || !strings.Contains(err.Error(), llm.EnvOpenRouterProviderSort) {
			t.Fatalf("err = %v, want a refusal naming %s", err, llm.EnvOpenRouterProviderSort)
		}
	})
}
