package settings

import (
	"context"
	"os"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/secret"
)

type fakeLister struct{ rows []sqlc.AuraSettings }

func (f fakeLister) List(context.Context) ([]sqlc.AuraSettings, error) { return f.rows, nil }

func TestAllowed(t *testing.T) {
	if m, ok := Allowed("OPENROUTER_API_KEY"); !ok || !m.Secret {
		t.Errorf("OPENROUTER_API_KEY should be allowed + secret, got ok=%v meta=%+v", ok, m)
	}
	for _, key := range []string{
		"AURA_LLM_BASE_URL",
		"AURA_LLM_MAX_TOKENS",
		"AURA_MODEL_CONTEXT_WINDOW",
		"AURA_MODEL_MAX_OUTPUT_TOKENS",
	} {
		if m, ok := Allowed(key); !ok || m.Secret || m.Kind != KindInt && key != "AURA_LLM_BASE_URL" {
			t.Errorf("%s should be an allowlisted non-secret model/token setting, got ok=%v meta=%+v", key, ok, m)
		}
	}
	if _, ok := Allowed("AURA_RERANK_MODEL"); !ok {
		t.Error("AURA_RERANK_MODEL should be allowed")
	}
	if m, ok := Allowed("AURA_LLM_PROVIDER"); !ok || m.Secret || m.Kind != KindString {
		t.Errorf("AURA_LLM_PROVIDER should be an allowlisted non-secret string setting, got ok=%v meta=%+v", ok, m)
	}
	if m, ok := Allowed("TELEGRAM_BOT_TOKEN"); !ok || !m.Secret {
		t.Errorf("TELEGRAM_BOT_TOKEN should be allowed + secret, got ok=%v meta=%+v", ok, m)
	}
	if _, ok := Allowed("POSTGRES_PASSWORD"); ok {
		t.Error("POSTGRES_PASSWORD must NOT be settings-overridable")
	}
}

func TestOverlayEnvAppliesAllowlistOnly(t *testing.T) {
	// A unique allowlisted key value + a clearly non-allowlisted key. Cleanup
	// restores the environment so other tests are unaffected.
	const allowed = "AURA_RERANK_MODEL"
	const denied = "AURA_NOT_A_REAL_OVERRIDE_KEY"
	prev, had := os.LookupEnv(allowed)
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(allowed, prev)
		} else {
			_ = os.Unsetenv(allowed)
		}
		_ = os.Unsetenv(denied)
	})

	l := fakeLister{rows: []sqlc.AuraSettings{
		{Key: allowed, Value: "cohere/rerank-4-fast"},
		{Key: denied, Value: "should-be-ignored"},
	}}
	if err := OverlayEnv(t.Context(), l); err != nil {
		t.Fatalf("OverlayEnv: %v", err)
	}
	if got := os.Getenv(allowed); got != "cohere/rerank-4-fast" {
		t.Errorf("%s = %q, want the overlaid value (DB wins)", allowed, got)
	}
	if got := os.Getenv(denied); got != "" {
		t.Errorf("%s = %q, want unset — non-allowlisted rows must be ignored", denied, got)
	}
}

func TestOverlayEnvFeedsRuntimeConfig(t *testing.T) {
	clearRuntimeConfigEnvForOverlayTest(t)

	l := fakeLister{rows: []sqlc.AuraSettings{
		{Key: "AURA_LLM_MODEL", Value: "settings/primary-model"},
		{Key: "AURA_LLM_BASE_URL", Value: "https://settings-llm.example/v1"},
		{Key: "OPENROUTER_API_KEY", Value: "sk-settings-overlay"},
		{Key: "AURA_LLM_MAX_TOKENS", Value: "1111"},
		{Key: "AURA_MODEL_CONTEXT_WINDOW", Value: "64000"},
		{Key: "AURA_MODEL_MAX_OUTPUT_TOKENS", Value: "4096"},
		{Key: "AURA_EMBED_MODEL", Value: "settings/embed-model"},
		{Key: "AURA_EMBED_BASE_URL", Value: "https://settings-embed.example"},
		{Key: "AURA_EMBED_DIMENSIONS", Value: "444"},
		{Key: "AURA_RERANK_MODEL", Value: "settings/rerank-model"},
		{Key: "AURA_RERANK_BASE_URL", Value: "https://settings-rerank.example"},
		{Key: "AURA_TTS_MODEL", Value: "settings-tts-model"},
		{Key: "AURA_STT_CLOUD_MODEL", Value: "settings-stt-model"},
		{Key: "AURA_VISION_CLOUD", Value: "true"},
	}}
	if err := OverlayEnv(t.Context(), l); err != nil {
		t.Fatalf("OverlayEnv: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load after OverlayEnv: %v", err)
	}

	if got := cfg.LLM.Model; got != "settings/primary-model" {
		t.Errorf("LLM.Model = %q, want overlaid settings model", got)
	}
	if got := cfg.LLM.BaseURL; got != "https://settings-llm.example/v1" {
		t.Errorf("LLM.BaseURL = %q, want overlaid settings base URL", got)
	}
	if got := cfg.LLM.APIKey; got != "sk-settings-overlay" {
		t.Errorf("LLM.APIKey = %q, want overlaid settings API key", got)
	}
	if got := cfg.LLM.MaxTokens; got != 1111 {
		t.Errorf("LLM.MaxTokens = %d, want 1111", got)
	}
	if got := cfg.LLM.ContextWindow; got != 64000 {
		t.Errorf("LLM.ContextWindow = %d, want 64000", got)
	}
	if got := cfg.LLM.MaxOutputTokens; got != 4096 {
		t.Errorf("LLM.MaxOutputTokens = %d, want 4096", got)
	}
	if got := cfg.Embed.Model; got != "settings/embed-model" {
		t.Errorf("Embed.Model = %q, want overlaid settings embed model", got)
	}
	if got := cfg.Embed.BaseURL; got != "https://settings-embed.example" {
		t.Errorf("Embed.BaseURL = %q, want overlaid settings embed base URL", got)
	}
	if got := cfg.Embed.Dimensions; got != 444 {
		t.Errorf("Embed.Dimensions = %d, want 444", got)
	}
	if got := cfg.RerankModel; got != "settings/rerank-model" {
		t.Errorf("RerankModel = %q, want overlaid settings rerank model", got)
	}
	if got := cfg.RerankBaseURL; got != "https://settings-rerank.example" {
		t.Errorf("RerankBaseURL = %q, want overlaid settings rerank base URL", got)
	}
	if got := cfg.TTSModel; got != "settings-tts-model" {
		t.Errorf("TTSModel = %q, want overlaid settings TTS model", got)
	}
	if got := cfg.STTCloudModel; got != "settings-stt-model" {
		t.Errorf("STTCloudModel = %q, want overlaid settings STT cloud model", got)
	}
	if !cfg.VisionCloud {
		t.Error("VisionCloud = false, want true from overlaid settings")
	}

	embedBase, embedKey, embedModel := cfg.EmbedRoute()
	if embedBase != "https://settings-embed.example" || embedKey != "sk-settings-overlay" || embedModel != "settings/embed-model" {
		t.Errorf("EmbedRoute() = (%q, %q, %q), want overlaid base/key/model", embedBase, embedKey, embedModel)
	}
	rerankBase, rerankKey, rerankModel := cfg.RerankRoute()
	if rerankBase != "https://settings-rerank.example" || rerankKey != "sk-settings-overlay" || rerankModel != "settings/rerank-model" {
		t.Errorf("RerankRoute() = (%q, %q, %q), want overlaid base/key/model", rerankBase, rerankKey, rerankModel)
	}
}

func TestOverlayEnvAppliesTelegramBotToken(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "")

	l := fakeLister{rows: []sqlc.AuraSettings{
		{Key: "TELEGRAM_BOT_TOKEN", Value: "123456:settings-telegram-token"},
	}}
	if err := OverlayEnv(t.Context(), l); err != nil {
		t.Fatalf("OverlayEnv: %v", err)
	}

	if got := os.Getenv("TELEGRAM_BOT_TOKEN"); got != "123456:settings-telegram-token" {
		t.Errorf("TELEGRAM_BOT_TOKEN = %q, want overlaid Settings token", got)
	}
}

func clearRuntimeConfigEnvForOverlayTest(t *testing.T) {
	t.Helper()
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	for _, key := range []string{
		"OPENROUTER_API_KEY",
		"AURA_LLM_PROVIDER",
		"AURA_LLM_MODEL",
		"AURA_LLM_BASE_URL",
		"AURA_LLM_TEMPERATURE",
		"AURA_LLM_MAX_TOKENS",
		"AURA_LLM_ADAPTIVE_REASONING",
		"AURA_SHOW_REASONING",
		"AURA_LLM_TOTAL_TIMEOUT_SEC",
		"AURA_LLM_CONNECT_TIMEOUT_SEC",
		"AURA_LLM_STREAM_IDLE_TIMEOUT_SEC",
		"AURA_MODEL_CONTEXT_WINDOW",
		"AURA_MODEL_MAX_OUTPUT_TOKENS",
		"AURA_COMPLETION_GATE",
		"AURA_COMPLETION_CRITIC_MODEL",
		"AURA_EMBED_MODEL",
		"AURA_EMBED_BASE_URL",
		"AURA_EMBED_DIMENSIONS",
		"AURA_RERANK_MODEL",
		"AURA_RERANK_BASE_URL",
		"AURA_TTS_MODEL",
		"AURA_STT_CLOUD_MODEL",
		"AURA_VISION_CLOUD",
	} {
		t.Setenv(key, "")
	}
}

// The two AURA_MEMORY_EMBED_* keys were sidecar-owned compose/.env vars whose
// cockpit overlay was a silent no-op (the daemon's os.Setenv cannot reach a
// running sidecar). They were removed from AllowedKeys; the agent-memory sidecar
// still consumes them from compose/.env at container start.
func TestMemoryEmbedKeysRemovedFromAllowlist(t *testing.T) {
	for _, key := range []string{"AURA_MEMORY_EMBED_BASE_URL", "AURA_MEMORY_EMBED_API_KEY"} {
		if _, ok := AllowedKeys[key]; ok {
			t.Errorf("%s must NOT be cockpit-overridable — it is a sidecar-owned compose/.env var", key)
		}
		if _, ok := Allowed(key); ok {
			t.Errorf("Allowed(%q) = true, want false after removal", key)
		}
	}
}

// A stale aura.settings row for a now-unlisted key (e.g. one an operator set
// before removal) must be silently skipped by OverlayEnv — no os.Setenv, no
// error — so no DB migration is needed to retire the key.
func TestOverlayEnvSkipsStaleRemovedKey(t *testing.T) {
	const stale = "AURA_MEMORY_EMBED_BASE_URL"
	if _, had := os.LookupEnv(stale); had {
		t.Fatalf("precondition: %s should be unset in the test env", stale)
	}
	t.Cleanup(func() { _ = os.Unsetenv(stale) })

	l := fakeLister{rows: []sqlc.AuraSettings{{Key: stale, Value: "https://stale.example/v1"}}}
	if err := OverlayEnv(t.Context(), l); err != nil {
		t.Fatalf("OverlayEnv with a stale removed-key row: %v", err)
	}
	if _, set := os.LookupEnv(stale); set {
		t.Errorf("%s was applied to the environment — a stale removed-key row must be ignored", stale)
	}
}

// Guard: removing the keys from the settings allowlist does not touch the
// independent secret.IsSecretEnvKey redaction predicate. The *_API_KEY / *_BASE_URL
// names still match the canonical marker set (B-09 superset), so any value that
// ever reaches a child env or exported profile is still redacted.
func TestSecretRedactionPredicateUnaffected(t *testing.T) {
	for _, key := range []string{
		"AURA_MEMORY_EMBED_API_KEY",
		"AURA_MEMORY_EMBED_BASE_URL",
		"OPENROUTER_API_KEY",
		"NEO4J_PASSWORD",
		"AURA_WEB_AUTH_SECRET",
	} {
		if !secret.IsSecretEnvKey(key) {
			t.Errorf("secret.IsSecretEnvKey(%q) = false, want true (redaction must stay intact)", key)
		}
	}
}
