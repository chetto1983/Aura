package config

import "testing"

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
