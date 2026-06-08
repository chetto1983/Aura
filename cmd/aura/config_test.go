package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withTempHome points HOME (and USERPROFILE on Windows) at a temp dir so
// ~/.aura/llm.json resolves under it, and clears OPENROUTER_API_KEY so the
// load-order chain is deterministic. It returns the resolved llm.json path.
func withTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows reads USERPROFILE
	t.Setenv("OPENROUTER_API_KEY", "")
	// Neutralize any AURA_LLM_* overrides leaking from the dev environment.
	for _, k := range []string{"AURA_LLM_MODEL", "AURA_LLM_BASE_URL", "AURA_LLM_TEMPERATURE", "AURA_LLM_MAX_TOKENS", "AURA_LLM_ADAPTIVE_REASONING"} {
		t.Setenv(k, "")
	}
	return filepath.Join(home, ".aura", "llm.json")
}

// TestConfig_SetCreatesFile asserts `set` creates ~/.aura/llm.json (dir + file)
// under a temp HOME and persists the file-tier key (D-24).
func TestConfig_SetCreatesFile(t *testing.T) {
	path := withTempHome(t)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("precondition: %s should not exist yet", path)
	}

	configSet([]string{"llm.model", "openai/gpt-test"})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written config: %v", err)
	}
	if !strings.Contains(string(data), "openai/gpt-test") {
		t.Fatalf("written config missing the model value:\n%s", data)
	}
}

// TestConfig_SetThenGet asserts a value written by `set` is read back by `get`
// through the full load-order chain (effective config).
func TestConfig_SetThenGet(t *testing.T) {
	withTempHome(t)

	configSet([]string{"llm.model", "openai/gpt-test"})

	cfg, err := loadLLMConfigTolerant()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got, ok := getConfigKey(cfg, "llm.model")
	if !ok {
		t.Fatal("getConfigKey llm.model: not ok")
	}
	if got != "openai/gpt-test" {
		t.Fatalf("get llm.model = %q, want openai/gpt-test", got)
	}
}

// TestConfig_SetNumericValidation asserts a bad numeric value is a clear error and
// is NOT persisted (operator typo must not be silently absorbed).
func TestConfig_SetNumericValidation(t *testing.T) {
	withTempHome(t)
	raw, err := readConfigFile("ignored")
	if err != nil {
		t.Fatalf("readConfigFile empty: %v", err)
	}
	if err := setConfigKey(raw, "llm.max_tokens", "not-a-number"); err == nil {
		t.Fatal("setConfigKey with bad int should error")
	}
	if err := setConfigKey(raw, "llm.temperature", "0.3"); err != nil {
		t.Fatalf("setConfigKey valid float: %v", err)
	}
}

// TestConfig_SetThenGetAdaptiveReasoning asserts the adaptive reasoning knob is
// available through the same file-tier config surface as the rest of llm.Config.
func TestConfig_SetThenGetAdaptiveReasoning(t *testing.T) {
	withTempHome(t)

	configSet([]string{"llm.adaptive_reasoning", "false"})

	cfg, err := loadLLMConfigTolerant()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got, ok := getConfigKey(cfg, "llm.adaptive_reasoning")
	if !ok {
		t.Fatal("getConfigKey llm.adaptive_reasoning: not ok")
	}
	if got != "false" {
		t.Fatalf("get llm.adaptive_reasoning = %q, want false", got)
	}
}

// TestConfig_SetRejectsAPIKey asserts the api_key is never settable via this file
// (D-28: it comes from .env / the environment).
func TestConfig_SetRejectsAPIKey(t *testing.T) {
	rawMap, _ := readConfigFile("ignored")
	if err := setConfigKey(rawMap, "llm.api_key", "sk-or-leak"); err == nil {
		t.Fatal("setting llm.api_key should be rejected")
	}
}

// TestConfig_ShowRedactsAPIKey asserts `show` renders the API key as REDACTED and
// the real key value NEVER appears in the output (T-03-01 release-blocking).
func TestConfig_ShowRedactsAPIKey(t *testing.T) {
	withTempHome(t)
	const realKey = "sk-or-SENTINEL-CONFIG-SHOW-DO-NOT-LEAK-998877"
	t.Setenv("OPENROUTER_API_KEY", realKey)

	out := captureStdout(t, configShow)

	if strings.Contains(out, realKey) {
		t.Fatalf("config show LEAKED the real API key:\n%s", out)
	}
	if !strings.Contains(out, redactedAPIKey) {
		t.Fatalf("config show did not render %q (api_key line):\n%s", redactedAPIKey, out)
	}
	if !strings.Contains(out, "model:") {
		t.Fatalf("config show missing the model line:\n%s", out)
	}
}

// TestConfig_ShowNoKeyBlank asserts `show` renders a blank api_key (not REDACTED)
// when no key is configured, and does not fail-fast on the empty key (D-24).
func TestConfig_ShowNoKeyBlank(t *testing.T) {
	withTempHome(t)
	out := captureStdout(t, configShow)
	if strings.Contains(out, redactedAPIKey) {
		t.Fatalf("config show should not print REDACTED when no key is set:\n%s", out)
	}
	if !strings.Contains(out, "model:") {
		t.Fatalf("config show missing the model line:\n%s", out)
	}
}
