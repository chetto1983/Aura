package main

import (
	"context"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

type settingsRuntimeClient struct{ route string }

func (*settingsRuntimeClient) Stream(context.Context, llm.Request) (<-chan llm.Chunk, error) {
	ch := make(chan llm.Chunk)
	close(ch)
	return ch, nil
}

func validFallbackLLMConfig() llm.Config {
	return llm.Config{
		Provider:          "openrouter",
		Model:             "cloud-model",
		BaseURL:           "https://openrouter.ai/api/v1",
		ContextWindow:     1_000_000,
		MaxTokens:         4096,
		MaxOutputTokens:   32768,
		TotalTimeoutSec:   120,
		ConnectTimeoutSec: 10,
	}
}

func localProfileServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gemma-4-12b","meta":{"n_ctx":81920}}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func ollamaProfileServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/show" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "" {
			http.Error(w, "authorization not accepted", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model_info":{"gemma4.context_length":262144}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPrimaryLLMRouteReloaderResolvesOverridesAndDeleteFallback(t *testing.T) {
	fallback := validFallbackLLMConfig()
	r := &primaryLLMRouteReloader{fallback: fallback}
	local, err := r.resolve(map[string]string{
		"AURA_LLM_PROVIDER": "llamacpp",
		"AURA_LLM_BASE_URL": "http://aura-llm:8084/v1",
		"AURA_LLM_MODEL":    "gemma-4-12b",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if local.Provider != "llamacpp" || local.BaseURL != "http://aura-llm:8084/v1" || local.Model != "gemma-4-12b" {
		t.Fatalf("local route = %q %q %q", local.Provider, local.BaseURL, local.Model)
	}

	reverted, err := r.resolve(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reverted.Provider != fallback.Provider || reverted.BaseURL != fallback.BaseURL || reverted.Model != fallback.Model {
		t.Fatalf("delete fallback = %q %q %q, want boot fallback", reverted.Provider, reverted.BaseURL, reverted.Model)
	}
}

// The API key is a hot profile row (amendment #188): the persisted row is what the
// runtime carries, and its absence means the pre-overlay boot key — never
// "whatever happened to be live", which is how a rotated key used to survive only
// until the next restart.
func TestPrimaryLLMRouteReloaderAppliesPersistedAPIKeyAndRevertsOnDelete(t *testing.T) {
	fallback := validFallbackLLMConfig()
	fallback.APIKey = "boot-env-key"
	active := fallback
	active.APIKey = "db-overlaid-key"
	r := &primaryLLMRouteReloader{
		fallback: fallback,
		runtime:  llm.NewRuntime(nil, active),
	}
	route := map[string]string{
		"AURA_LLM_PROVIDER": "llamacpp",
		"AURA_LLM_BASE_URL": "http://aura-llm:8084/v1",
		"AURA_LLM_MODEL":    "gemma-4-12b",
	}
	withRow := maps.Clone(route)
	withRow["OPENROUTER_API_KEY"] = " rotated-key "
	got, err := r.resolve(withRow, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.APIKey != "rotated-key" {
		t.Fatalf("api key = %q, want the persisted row applied (trimmed)", got.APIKey)
	}
	reverted, err := r.resolve(route, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reverted.APIKey != "boot-env-key" {
		t.Fatalf("api key after delete = %q, want boot fallback", reverted.APIKey)
	}
}

// The loop budget and the compaction trigger ride the same profile (amendment
// #188): pinned rows publish into the snapshot, absent rows revert to the boot
// value (0 = env/default for the loop), and each is range-checked before persistence.
func TestPrimaryLLMRouteReloaderResolvesLoopBudgetAndCompactionTrigger(t *testing.T) {
	fallback := validFallbackLLMConfig()
	fallback.CompactionTriggerPercent = 50
	r := &primaryLLMRouteReloader{fallback: fallback, runtime: llm.NewRuntime(nil, fallback)}
	got, err := r.resolve(map[string]string{
		"AURA_LOOP_MAX_STEPS":                     "60",
		"AURA_LOOP_MAX_WALLCLOCK_SEC":             "1200",
		"AURA_CONTEXT_COMPACTION_TRIGGER_PERCENT": "0",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.LoopMaxSteps != 60 || got.LoopMaxWallclockSec != 1200 || got.CompactionTriggerPercent != 0 {
		t.Fatalf("resolved loop/trigger = %d/%d/%d", got.LoopMaxSteps, got.LoopMaxWallclockSec, got.CompactionTriggerPercent)
	}
	reverted, err := r.resolve(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reverted.LoopMaxSteps != 0 || reverted.LoopMaxWallclockSec != 0 || reverted.CompactionTriggerPercent != 50 {
		t.Fatalf("delete fallback loop/trigger = %d/%d/%d, want 0/0/50", reverted.LoopMaxSteps, reverted.LoopMaxWallclockSec, reverted.CompactionTriggerPercent)
	}
	for key, bad := range map[string]string{
		"AURA_LOOP_MAX_STEPS":                     "0",
		"AURA_LOOP_MAX_WALLCLOCK_SEC":             "-5",
		"AURA_CONTEXT_COMPACTION_TRIGGER_PERCENT": "101",
	} {
		if _, err := r.resolve(map[string]string{key: bad}, nil); err == nil {
			t.Fatalf("%s=%q accepted, want a range error", key, bad)
		}
	}

	r.runtime.Replace(nil, got)
	if v, ok := r.EffectiveValue("AURA_LOOP_MAX_STEPS"); !ok || v != "60" {
		t.Fatalf("effective steps = (%q,%v)", v, ok)
	}
	if v, ok := r.EffectiveValue("AURA_CONTEXT_COMPACTION_TRIGGER_PERCENT"); !ok || v != "0" {
		t.Fatalf("effective trigger = (%q,%v)", v, ok)
	}
	r.runtime.Replace(nil, reverted)
	if _, ok := r.EffectiveValue("AURA_LOOP_MAX_STEPS"); ok {
		t.Fatal("an unpinned loop budget must fall through to the process env, not report 0")
	}
}

func TestPrimaryLLMRouteReloaderApplyPublishesRuntimeSnapshot(t *testing.T) {
	srv := localProfileServer(t)
	oldClient := &settingsRuntimeClient{route: "old"}
	runtime := llm.NewRuntime(oldClient, validFallbackLLMConfig())
	r := &primaryLLMRouteReloader{fallback: validFallbackLLMConfig(), runtime: runtime}

	apply, err := r.Prepare(context.Background(), map[string]string{
		"AURA_LLM_PROVIDER": "llamacpp",
		"AURA_LLM_BASE_URL": srv.URL + "/v1",
		"AURA_LLM_MODEL":    "gemma-4-12b",
	}, []string{"AURA_MODEL_CONTEXT_WINDOW", "AURA_MODEL_MAX_OUTPUT_TOKENS"})
	if err != nil {
		t.Fatal(err)
	}
	apply()

	got := runtime.Snapshot().Config
	if got.Provider != "llamacpp" || got.BaseURL != srv.URL+"/v1" || got.Model != "gemma-4-12b" {
		t.Fatalf("published route = %q %q %q", got.Provider, got.BaseURL, got.Model)
	}
	if got.ContextWindow != 81920 {
		t.Fatalf("context window = %d, want live llama.cpp n_ctx 81920", got.ContextWindow)
	}
	if display, ok := llm.CostUSD(got.Prices, got.Model, llm.Usage{PromptTokens: 100}); !ok || display != "$0.000000" {
		t.Fatalf("local cost = (%q,%v), want explicit included zero", display, ok)
	}
}

func TestPrimaryLLMRouteReloaderPublishesOllamaCloudProfile(t *testing.T) {
	srv := ollamaProfileServer(t)
	fallback := validFallbackLLMConfig()
	fallback.APIKey = "retained-cloud-key"
	fallback.ContextWindowConfigured = true
	fallback.MaxOutputTokensConfigured = true
	runtime := llm.NewRuntime(&settingsRuntimeClient{route: "old"}, fallback)
	r := &primaryLLMRouteReloader{fallback: fallback, runtime: runtime}

	apply, err := r.Prepare(context.Background(), map[string]string{
		"AURA_LLM_PROVIDER": "ollama",
		"AURA_LLM_BASE_URL": srv.URL + "/v1",
		"AURA_LLM_MODEL":    "gemma4:31b-cloud",
	}, []string{"AURA_MODEL_CONTEXT_WINDOW", "AURA_MODEL_MAX_OUTPUT_TOKENS"})
	if err != nil {
		t.Fatal(err)
	}
	apply()

	got := runtime.Snapshot().Config
	if got.Provider != "ollama" || got.ContextWindow != 262144 {
		t.Fatalf("published Ollama profile = provider %q, context %d", got.Provider, got.ContextWindow)
	}
	if got.ContextWindowConfigured || got.MaxOutputTokensConfigured {
		t.Fatalf("inherited startup limits remained configured: context=%v output=%v", got.ContextWindowConfigured, got.MaxOutputTokensConfigured)
	}
	if got.CostStatus != llm.CostStatusSubscriptionIncluded {
		t.Fatalf("cost status = %q", got.CostStatus)
	}
	if _, priced := got.Prices[got.Model]; priced {
		t.Fatal("Ollama cloud profile inherited a numeric price")
	}
}

// A hot write of a non-route key (loop budget, trigger, API key) after a route
// transition must keep the provider-discovered model limits: the startup .env pin is
// the boot fallback, not something every later write restores (amendment #196).
func TestPrimaryLLMRouteReloaderKeepsDiscoveredLimitsAcrossNonRouteWrites(t *testing.T) {
	srv := ollamaProfileServer(t)
	fallback := validFallbackLLMConfig()
	fallback.ContextWindowConfigured = true
	fallback.MaxOutputTokensConfigured = true
	runtime := llm.NewRuntime(&settingsRuntimeClient{route: "old"}, fallback)
	r := &primaryLLMRouteReloader{fallback: fallback, runtime: runtime}
	route := map[string]string{
		"AURA_LLM_PROVIDER": "ollama",
		"AURA_LLM_BASE_URL": srv.URL + "/v1",
		"AURA_LLM_MODEL":    "gemma4:31b-cloud",
	}
	transition, err := r.Prepare(context.Background(), route, []string{"AURA_MODEL_CONTEXT_WINDOW", "AURA_MODEL_MAX_OUTPUT_TOKENS"})
	if err != nil {
		t.Fatal(err)
	}
	transition()

	later := maps.Clone(route)
	later["AURA_LOOP_MAX_STEPS"] = "60"
	apply, err := r.Prepare(context.Background(), later, nil)
	if err != nil {
		t.Fatal(err)
	}
	apply()

	got := runtime.Snapshot().Config
	if got.ContextWindow != 262144 || got.ContextWindowConfigured {
		t.Fatalf("loop-budget write restored the startup context pin: window=%d configured=%v", got.ContextWindow, got.ContextWindowConfigured)
	}
	if got.MaxOutputTokensConfigured {
		t.Fatal("loop-budget write restored the startup max-output pin")
	}
	if got.LoopMaxSteps != 60 {
		t.Fatalf("loop budget = %d, want 60", got.LoopMaxSteps)
	}
}

func TestPrimaryLLMRouteReloaderRejectsInvalidRoute(t *testing.T) {
	r := &primaryLLMRouteReloader{fallback: validFallbackLLMConfig()}
	for name, overrides := range map[string]map[string]string{
		"provider": {"AURA_LLM_PROVIDER": "unknown"},
		"model":    {"AURA_LLM_MODEL": " "},
		"base URL": {"AURA_LLM_BASE_URL": "aura-llm:8084/v1"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := r.Prepare(context.Background(), overrides, nil); err == nil {
				t.Fatal("Prepare accepted an invalid primary route")
			}
		})
	}
}
