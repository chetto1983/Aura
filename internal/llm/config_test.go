package llm_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// clearLLMEnv unsets every var Load reads so a test starts from the built-in
// defaults regardless of the developer's shell or a leaked .env. t.Setenv
// restores them at test end.
func clearLLMEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"OPENROUTER_API_KEY",
		"AURA_LLM_MODEL",
		"AURA_LLM_BASE_URL",
		"AURA_LLM_TEMPERATURE",
		"AURA_LLM_MAX_TOKENS",
		"AURA_LLM_TOTAL_TIMEOUT_SEC",
		"AURA_LLM_CONNECT_TIMEOUT_SEC",
	} {
		t.Setenv(k, "")
	}
}

// isolateHome points HOME/USERPROFILE at an empty temp dir so configFilePath
// resolves to a non-existent ~/.aura/llm.json (the absent-file path) unless a
// test explicitly writes one. Also neutralizes a stray repo-root .env that
// godotenv.Load would otherwise pick up.
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // Windows
	t.Chdir(home)                 // godotenv.Load() looks in cwd; keep it clean
	return home
}

func TestConfigLoadOrder(t *testing.T) {
	t.Run("defaults_with_only_api_key", func(t *testing.T) {
		isolateHome(t)
		clearLLMEnv(t)
		t.Setenv("OPENROUTER_API_KEY", "sk-test-default")

		cfg, err := llm.Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Model != "deepseek/deepseek-v4-flash:exacto" {
			t.Errorf("Model = %q, want deepseek/deepseek-v4-flash:exacto", cfg.Model)
		}
		if cfg.BaseURL != "https://openrouter.ai/api/v1" {
			t.Errorf("BaseURL = %q, want https://openrouter.ai/api/v1", cfg.BaseURL)
		}
		if cfg.Provider != "openrouter" {
			t.Errorf("Provider = %q, want openrouter", cfg.Provider)
		}
		if cfg.Temperature != 0.7 {
			t.Errorf("Temperature = %v, want 0.7", cfg.Temperature)
		}
		if cfg.MaxTokens != 4096 {
			t.Errorf("MaxTokens = %d, want 4096", cfg.MaxTokens)
		}
		if cfg.TotalTimeoutSec != 120 || cfg.ConnectTimeoutSec != 10 {
			t.Errorf("timeouts = %d/%d, want 120/10", cfg.TotalTimeoutSec, cfg.ConnectTimeoutSec)
		}
		if cfg.Headers["HTTP-Referer"] == "" || cfg.Headers["X-Title"] != "Aura" {
			t.Errorf("attribution headers missing: %#v", cfg.Headers)
		}
	})

	t.Run("file_overrides_default_env_overrides_file", func(t *testing.T) {
		home := isolateHome(t)
		clearLLMEnv(t)
		t.Setenv("OPENROUTER_API_KEY", "sk-test-precedence")

		// Tier 3: ~/.aura/llm.json sets model to a file value.
		auraDir := filepath.Join(home, ".aura")
		if err := os.MkdirAll(auraDir, 0o755); err != nil {
			t.Fatal(err)
		}
		const fileModel = "file/model:from-json"
		js := `{"model":"` + fileModel + `","base_url":"https://file.example/v1","temperature":0.3}`
		if err := os.WriteFile(filepath.Join(auraDir, "llm.json"), []byte(js), 0o600); err != nil {
			t.Fatal(err)
		}

		// File-only (no AURA_LLM_MODEL): the file value wins over the default.
		cfg, err := llm.Load()
		if err != nil {
			t.Fatalf("Load (file tier): %v", err)
		}
		if cfg.Model != fileModel {
			t.Errorf("file tier: Model = %q, want %q", cfg.Model, fileModel)
		}
		if cfg.BaseURL != "https://file.example/v1" {
			t.Errorf("file tier: BaseURL = %q, want https://file.example/v1", cfg.BaseURL)
		}
		if cfg.Temperature != 0.3 {
			t.Errorf("file tier: Temperature = %v, want 0.3", cfg.Temperature)
		}

		// Tier 4: AURA_LLM_MODEL overrides the file value.
		const envModel = "env/model:wins"
		t.Setenv("AURA_LLM_MODEL", envModel)
		cfg2, err := llm.Load()
		if err != nil {
			t.Fatalf("Load (env tier): %v", err)
		}
		if cfg2.Model != envModel {
			t.Errorf("env tier: Model = %q, want %q (env must win over file)", cfg2.Model, envModel)
		}
		// The file's base_url still applies (no env override for it).
		if cfg2.BaseURL != "https://file.example/v1" {
			t.Errorf("env tier: BaseURL = %q, want the file value to persist", cfg2.BaseURL)
		}
	})
}

func TestConfigMissingKey(t *testing.T) {
	isolateHome(t)
	clearLLMEnv(t)
	// No OPENROUTER_API_KEY anywhere → clear non-panic sentinel error.

	cfg, err := llm.Load()
	if cfg != nil {
		t.Errorf("Load returned a non-nil Config on empty key: %#v", cfg)
	}
	if !errors.Is(err, llm.ErrMissingAPIKey) {
		t.Fatalf("err = %v, want ErrMissingAPIKey", err)
	}
}

func TestConfigMalformedEnvFailsFast(t *testing.T) {
	isolateHome(t)
	clearLLMEnv(t)
	t.Setenv("OPENROUTER_API_KEY", "sk-test")
	t.Setenv("AURA_LLM_MAX_TOKENS", "not-an-int")

	if _, err := llm.Load(); err == nil {
		t.Fatal("Load: want fail-fast error on malformed AURA_LLM_MAX_TOKENS, got nil")
	}
}
