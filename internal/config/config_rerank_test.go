package config

import (
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// TestLoad_RerankBaseURL asserts the Phase 30 (RET-01) rerank knob: the
// AURA_RERANK_BASE_URL default lands when unset and the env override is honored
// (mirrors the AURA_EMBED_BASE_URL default/override coverage). It lives in its
// own file so config_test.go stays under the 600-LOC cap (CLAUDE.md no-god-class).
func TestLoad_RerankBaseURL(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		clearPostgresEnv(t)
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load returned error: %v", err)
		}
		if cfg.RerankBaseURL != "http://127.0.0.1:8085" {
			t.Errorf("RerankBaseURL: want default http://127.0.0.1:8085, got %q", cfg.RerankBaseURL)
		}
	})
	t.Run("override", func(t *testing.T) {
		clearPostgresEnv(t)
		t.Setenv("AURA_RERANK_BASE_URL", "http://rerank.internal:9100")
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load returned error: %v", err)
		}
		if cfg.RerankBaseURL != "http://rerank.internal:9100" {
			t.Errorf("RerankBaseURL override not applied: %q", cfg.RerankBaseURL)
		}
	})
}

func TestRerankRoute(t *testing.T) {
	t.Run("local sidecar when model unset", func(t *testing.T) {
		cfg := &Config{RerankBaseURL: "http://127.0.0.1:8085"}
		base, key, model := cfg.RerankRoute()
		if base != "http://127.0.0.1:8085" || key != "" || model != "" {
			t.Fatalf("RerankRoute local = (%q,%q,%q), want local base with no auth/model", base, key, model)
		}
	})

	t.Run("model swaps loopback base to shared llm route", func(t *testing.T) {
		cfg := &Config{
			RerankBaseURL: "http://127.0.0.1:8085",
			RerankModel:   "cohere/rerank-4-fast",
			LLM:           llm.Config{BaseURL: "https://openrouter.ai/api/v1", APIKey: "shared-key"},
		}
		base, key, model := cfg.RerankRoute()
		// The shared OpenRouter base has its trailing /v1 stripped: the rerank
		// client appends "/v1/rerank", so a base of ".../api/v1" would build the
		// double-/v1 ".../api/v1/v1/rerank" that 404s (and the fail-soft client
		// silently degrades to identity). The route must yield ".../api" so the
		// final URL is the validated ".../api/v1/rerank".
		if base != "https://openrouter.ai/api" || key != "shared-key" || model != "cohere/rerank-4-fast" {
			t.Fatalf("RerankRoute cloud = (%q,%q,%q), want shared llm route with /v1 stripped", base, key, model)
		}
	})

	t.Run("custom non-loopback base wins", func(t *testing.T) {
		cfg := &Config{
			RerankBaseURL: "https://rerank.example.test",
			RerankModel:   "custom-model",
			LLM:           llm.Config{BaseURL: "https://openrouter.ai/api/v1", APIKey: "shared-key"},
		}
		base, key, model := cfg.RerankRoute()
		if base != "https://rerank.example.test" || key != "shared-key" || model != "custom-model" {
			t.Fatalf("RerankRoute custom = (%q,%q,%q), want custom base with shared key/model", base, key, model)
		}
	})
}

func TestEmbedRoute(t *testing.T) {
	t.Run("local sidecar when model unset", func(t *testing.T) {
		cfg := &Config{Embed: EmbedConfig{BaseURL: "http://127.0.0.1:8081"}}
		base, key, model := cfg.EmbedRoute()
		if base != "http://127.0.0.1:8081" || key != "" || model != "" {
			t.Fatalf("EmbedRoute local = (%q,%q,%q), want local base with no auth/model", base, key, model)
		}
	})

	t.Run("model swaps loopback base to shared llm route with /v1 stripped", func(t *testing.T) {
		cfg := &Config{
			Embed: EmbedConfig{BaseURL: "http://127.0.0.1:8081", Model: "qwen/qwen3-embedding-8b"},
			LLM:   llm.Config{BaseURL: "https://openrouter.ai/api/v1", APIKey: "shared-key"},
		}
		base, key, model := cfg.EmbedRoute()
		// Same /v1-strip contract as RerankRoute: the embed client appends
		// "/v1/embeddings", so the base must be ".../api" → ".../api/v1/embeddings".
		if base != "https://openrouter.ai/api" || key != "shared-key" || model != "qwen/qwen3-embedding-8b" {
			t.Fatalf("EmbedRoute cloud = (%q,%q,%q), want shared llm route with /v1 stripped", base, key, model)
		}
	})

	t.Run("custom non-loopback embed base wins", func(t *testing.T) {
		cfg := &Config{
			Embed: EmbedConfig{BaseURL: "https://embed.example.test", Model: "custom-embed"},
			LLM:   llm.Config{BaseURL: "https://openrouter.ai/api/v1", APIKey: "shared-key"},
		}
		base, key, model := cfg.EmbedRoute()
		if base != "https://embed.example.test" || key != "shared-key" || model != "custom-embed" {
			t.Fatalf("EmbedRoute custom = (%q,%q,%q), want custom base with shared key/model", base, key, model)
		}
	})
}
