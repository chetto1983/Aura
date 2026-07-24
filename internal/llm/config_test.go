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
		"AURA_LLM_OPENROUTER_MIDDLE_OUT",
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
		if cfg.Model != "deepseek/deepseek-v4-flash:nitro" {
			t.Errorf("Model = %q, want deepseek/deepseek-v4-flash:nitro", cfg.Model)
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
		if !cfg.AdaptiveReasoning {
			t.Error("AdaptiveReasoning = false, want true default")
		}
		if !cfg.ShowReasoning {
			t.Error("ShowReasoning = false, want true default (live CoT on by default)")
		}
		if cfg.TotalTimeoutSec != 120 || cfg.ConnectTimeoutSec != 10 {
			t.Errorf("timeouts = %d/%d, want 120/10", cfg.TotalTimeoutSec, cfg.ConnectTimeoutSec)
		}
		if cfg.StreamIdleTimeoutSec != 60 {
			t.Errorf("StreamIdleTimeoutSec = %d, want 60 default (B-08)", cfg.StreamIdleTimeoutSec)
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
		js := `{"model":"` + fileModel + `","base_url":"https://file.example/v1","temperature":0.3,"adaptive_reasoning":false}`
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
		if cfg.AdaptiveReasoning {
			t.Error("file tier: AdaptiveReasoning = true, want false")
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
		if cfg2.AdaptiveReasoning {
			t.Error("env tier: AdaptiveReasoning = true, want file false to persist")
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

func TestLoadAllowEmptyKeyAllowsEmptyAPIKey(t *testing.T) {
	isolateHome(t)
	clearLLMEnv(t)

	cfg, err := llm.LoadAllowEmptyKey()
	if err != nil {
		t.Fatalf("LoadAllowEmptyKey: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadAllowEmptyKey returned nil config")
	}
	if cfg.APIKey != "" {
		t.Fatalf("APIKey = %q, want empty", cfg.APIKey)
	}
	if cfg.Model != "deepseek/deepseek-v4-flash:nitro" {
		t.Fatalf("Model = %q, want default model", cfg.Model)
	}

	if _, err := llm.Load(); !errors.Is(err, llm.ErrMissingAPIKey) {
		t.Fatalf("Load() with the same empty key err = %v, want ErrMissingAPIKey", err)
	}
}

// TestConfigEnvOpenRouterMiddleOut locks the fix-plan 1.11 knob: default OFF
// (dormant belt, byte-unchanged wire), set-true flips it on, and a malformed
// value falls back to the current value without blocking boot.
func TestConfigEnvOpenRouterMiddleOut(t *testing.T) {
	t.Run("unset_defaults_off", func(t *testing.T) {
		isolateHome(t)
		clearLLMEnv(t)
		t.Setenv("OPENROUTER_API_KEY", "sk-test-middleout-default")

		cfg, err := llm.Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.OpenRouterMiddleOut {
			t.Error("OpenRouterMiddleOut = true, want false default (opt-in belt)")
		}
	})

	t.Run("set_true_overrides_default", func(t *testing.T) {
		isolateHome(t)
		clearLLMEnv(t)
		t.Setenv("OPENROUTER_API_KEY", "sk-test-middleout-on")
		t.Setenv("AURA_LLM_OPENROUTER_MIDDLE_OUT", "true")

		cfg, err := llm.Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !cfg.OpenRouterMiddleOut {
			t.Error("OpenRouterMiddleOut = false, want true from AURA_LLM_OPENROUTER_MIDDLE_OUT=true")
		}
	})

	t.Run("malformed_falls_back", func(t *testing.T) {
		isolateHome(t)
		clearLLMEnv(t)
		t.Setenv("OPENROUTER_API_KEY", "sk-test-middleout-malformed")
		t.Setenv("AURA_LLM_OPENROUTER_MIDDLE_OUT", "not-a-bool")

		cfg, err := llm.Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.OpenRouterMiddleOut {
			t.Error("OpenRouterMiddleOut = true, want false fallback on a malformed value")
		}
	})
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

// TestConfigMalformedNumericEnv covers every fail-fast numeric env knob (the
// float and the three int knobs), so a single operator typo in any of them is a
// loud Load error rather than a silently-absorbed default.
func TestConfigMalformedNumericEnv(t *testing.T) {
	cases := []struct {
		key, bad string
	}{
		{"AURA_LLM_TEMPERATURE", "hot"},
		{"AURA_LLM_MAX_TOKENS", "lots"},
		{"AURA_LLM_TOTAL_TIMEOUT_SEC", "soon"},
		{"AURA_LLM_CONNECT_TIMEOUT_SEC", "fast"},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			isolateHome(t)
			clearLLMEnv(t)
			t.Setenv("OPENROUTER_API_KEY", "sk-test")
			t.Setenv(tc.key, tc.bad)

			if _, err := llm.Load(); err == nil {
				t.Fatalf("Load: want fail-fast error on malformed %s=%q, got nil", tc.key, tc.bad)
			}
		})
	}
}

// TestConfigEnvProviderOverride locks the net-new AURA_LLM_PROVIDER env knob (OQ-1):
// set → cfg.Provider takes the env value so llama.cpp is positively identifiable at
// request time; unset → the built-in "openrouter" default is untouched (no regression).
func TestConfigEnvProviderOverride(t *testing.T) {
	t.Run("set_overrides_default", func(t *testing.T) {
		isolateHome(t)
		clearLLMEnv(t)
		t.Setenv("OPENROUTER_API_KEY", "sk-test-provider")
		t.Setenv("AURA_LLM_PROVIDER", "llamacpp")

		cfg, err := llm.Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Provider != "llamacpp" {
			t.Errorf("Provider = %q, want llamacpp from AURA_LLM_PROVIDER", cfg.Provider)
		}
	})

	t.Run("unset_keeps_default", func(t *testing.T) {
		isolateHome(t)
		clearLLMEnv(t)
		t.Setenv("OPENROUTER_API_KEY", "sk-test-provider-default")

		cfg, err := llm.Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Provider != "openrouter" {
			t.Errorf("Provider = %q, want the untouched openrouter default", cfg.Provider)
		}
	})
}

// TestConfigEnvNumericOverrides asserts every valid numeric env value overrides
// the lower tiers — the float (temperature) and all three int knobs — so the
// happy-path branch of each fail-fast reader is exercised too.
func TestConfigEnvNumericOverrides(t *testing.T) {
	isolateHome(t)
	clearLLMEnv(t)
	t.Setenv("OPENROUTER_API_KEY", "sk-test-numeric")
	t.Setenv("AURA_LLM_TEMPERATURE", "0.15")
	t.Setenv("AURA_LLM_MAX_TOKENS", "256")
	t.Setenv("AURA_LLM_TOTAL_TIMEOUT_SEC", "45")
	t.Setenv("AURA_LLM_CONNECT_TIMEOUT_SEC", "7")
	t.Setenv("AURA_LLM_STREAM_IDLE_TIMEOUT_SEC", "33")
	t.Setenv("AURA_LLM_BASE_URL", "https://env.example/v1")
	t.Setenv("AURA_LLM_ADAPTIVE_REASONING", "false")

	cfg, err := llm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Temperature != 0.15 {
		t.Errorf("Temperature = %v, want 0.15", cfg.Temperature)
	}
	if cfg.MaxTokens != 256 {
		t.Errorf("MaxTokens = %d, want 256", cfg.MaxTokens)
	}
	if cfg.TotalTimeoutSec != 45 || cfg.ConnectTimeoutSec != 7 {
		t.Errorf("timeouts = %d/%d, want 45/7", cfg.TotalTimeoutSec, cfg.ConnectTimeoutSec)
	}
	if cfg.StreamIdleTimeoutSec != 33 {
		t.Errorf("StreamIdleTimeoutSec = %d, want 33 from env override", cfg.StreamIdleTimeoutSec)
	}
	if cfg.BaseURL != "https://env.example/v1" {
		t.Errorf("BaseURL = %q, want the env value", cfg.BaseURL)
	}
	if cfg.AdaptiveReasoning {
		t.Error("AdaptiveReasoning = true, want env false")
	}
}

// TestConfigFileOverlayAllFields exercises every overlayFile branch (the pointer
// fields plus the headers + prices maps) and the file-tier api_key path, so a
// file-only configuration (no AURA_LLM_* / OPENROUTER_API_KEY env) fully resolves.
func TestConfigFileOverlayAllFields(t *testing.T) {
	home := isolateHome(t)
	clearLLMEnv(t)
	// No OPENROUTER_API_KEY in the env — the file's api_key must satisfy the chain.

	auraDir := filepath.Join(home, ".aura")
	if err := os.MkdirAll(auraDir, 0o755); err != nil {
		t.Fatal(err)
	}
	js := `{
	  "provider": "file-prov",
	  "model": "file/model",
	  "base_url": "https://file.example/v1",
	  "api_key": "sk-file-key",
	  "temperature": 0.42,
	  "max_tokens": 333,
	  "adaptive_reasoning": false,
	  "total_timeout_sec": 99,
	  "connect_timeout_sec": 11,
	  "headers": {"X-Extra": "from-file"},
	  "prices": {"file/model": {"input_per_1m": 1.5, "output_per_1m": 2.5}}
	}`
	if err := os.WriteFile(filepath.Join(auraDir, "llm.json"), []byte(js), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := llm.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Provider != "file-prov" || cfg.Model != "file/model" {
		t.Errorf("provider/model = %q/%q, want file-prov/file/model", cfg.Provider, cfg.Model)
	}
	if cfg.APIKey != "sk-file-key" {
		t.Errorf("APIKey = %q, want the file-tier key", cfg.APIKey)
	}
	if cfg.BaseURL != "https://file.example/v1" {
		t.Errorf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.Temperature != 0.42 || cfg.MaxTokens != 333 {
		t.Errorf("temperature/max_tokens = %v/%d, want 0.42/333", cfg.Temperature, cfg.MaxTokens)
	}
	if cfg.AdaptiveReasoning {
		t.Error("AdaptiveReasoning = true, want file false")
	}
	if cfg.TotalTimeoutSec != 99 || cfg.ConnectTimeoutSec != 11 {
		t.Errorf("timeouts = %d/%d, want 99/11", cfg.TotalTimeoutSec, cfg.ConnectTimeoutSec)
	}
	if cfg.Headers["X-Extra"] != "from-file" {
		t.Errorf("merged header missing: %#v", cfg.Headers)
	}
	if cfg.Headers["X-Title"] != "Aura" {
		t.Errorf("default attribution header clobbered by the file overlay: %#v", cfg.Headers)
	}
	if p := cfg.Prices["file/model"]; p.InputPer1M != 1.5 || p.OutputPer1M != 2.5 {
		t.Errorf("price overlay = %+v, want 1.5/2.5", p)
	}
	if _, ok := cfg.Prices["deepseek/deepseek-v4-flash:nitro"]; !ok {
		t.Error("seeded price entry dropped by the entry-by-entry overlay")
	}
}

// TestConfigFileReadErrorNotNotExist asserts a non-ErrNotExist read failure
// (here: llm.json is a directory, not a file) surfaces a loud error rather than
// being swallowed like the absent-file case.
func TestConfigFileReadErrorNotNotExist(t *testing.T) {
	home := isolateHome(t)
	clearLLMEnv(t)
	t.Setenv("OPENROUTER_API_KEY", "sk-test")

	// Make ~/.aura/llm.json a DIRECTORY so os.ReadFile returns a non-NotExist error.
	dirAsFile := filepath.Join(home, ".aura", "llm.json")
	if err := os.MkdirAll(dirAsFile, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := llm.Load(); err == nil {
		t.Fatal("Load: want a loud read error when llm.json is a directory, got nil")
	}
}

// TestConfigNoHomeIsCleanNoOp asserts that when the home dir cannot be resolved
// (HOME + USERPROFILE both empty) configFilePath yields "" and the file tier is a
// clean no-op — Load still resolves from env (no panic, no error from the missing
// file). On platforms where os.UserHomeDir falls back to other sources this still
// holds: an unresolved path must never wedge Load.
func TestConfigNoHomeIsCleanNoOp(t *testing.T) {
	clearLLMEnv(t)
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("OPENROUTER_API_KEY", "sk-test-nohome")
	// Keep godotenv.Load() from picking up a stray repo .env.
	t.Chdir(t.TempDir())

	cfg, err := llm.Load()
	if err != nil {
		t.Fatalf("Load with no resolvable home must not error: %v", err)
	}
	if cfg.APIKey != "sk-test-nohome" {
		t.Errorf("APIKey = %q, want the env value (env tier still applies)", cfg.APIKey)
	}
}

// TestConfigMalformedFileFailsLoud asserts a present-but-unparseable llm.json is a
// loud error (a corrupt config must never be silently ignored).
func TestConfigMalformedFileFailsLoud(t *testing.T) {
	home := isolateHome(t)
	clearLLMEnv(t)
	t.Setenv("OPENROUTER_API_KEY", "sk-test")

	auraDir := filepath.Join(home, ".aura")
	if err := os.MkdirAll(auraDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(auraDir, "llm.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := llm.Load(); err == nil {
		t.Fatal("Load: want a loud parse error on a corrupt llm.json, got nil")
	}
}
