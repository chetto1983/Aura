package main

import (
	"context"
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
	})
	if err != nil {
		t.Fatal(err)
	}
	if local.Provider != "llamacpp" || local.BaseURL != "http://aura-llm:8084/v1" || local.Model != "gemma-4-12b" {
		t.Fatalf("local route = %q %q %q", local.Provider, local.BaseURL, local.Model)
	}

	reverted, err := r.resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	if reverted.Provider != fallback.Provider || reverted.BaseURL != fallback.BaseURL || reverted.Model != fallback.Model {
		t.Fatalf("delete fallback = %q %q %q, want boot fallback", reverted.Provider, reverted.BaseURL, reverted.Model)
	}
}

func TestPrimaryLLMRouteReloaderPreservesActiveNonProfileSecrets(t *testing.T) {
	fallback := validFallbackLLMConfig()
	active := fallback
	active.APIKey = "db-overlaid-key"
	r := &primaryLLMRouteReloader{
		fallback: fallback,
		runtime:  llm.NewRuntime(nil, active),
	}
	got, err := r.resolve(map[string]string{
		"AURA_LLM_PROVIDER": "llamacpp",
		"AURA_LLM_BASE_URL": "http://aura-llm:8084/v1",
		"AURA_LLM_MODEL":    "gemma-4-12b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.APIKey != "db-overlaid-key" {
		t.Fatal("hot profile discarded the active DB-overlaid API key")
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
	})
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
	runtime := llm.NewRuntime(&settingsRuntimeClient{route: "old"}, fallback)
	r := &primaryLLMRouteReloader{fallback: fallback, runtime: runtime}

	apply, err := r.Prepare(context.Background(), map[string]string{
		"AURA_LLM_PROVIDER": "ollama",
		"AURA_LLM_BASE_URL": srv.URL + "/v1",
		"AURA_LLM_MODEL":    "gemma4:31b-cloud",
	})
	if err != nil {
		t.Fatal(err)
	}
	apply()

	got := runtime.Snapshot().Config
	if got.Provider != "ollama" || got.ContextWindow != 262144 {
		t.Fatalf("published Ollama profile = provider %q, context %d", got.Provider, got.ContextWindow)
	}
	if got.CostStatus != llm.CostStatusSubscriptionIncluded {
		t.Fatalf("cost status = %q", got.CostStatus)
	}
	if _, priced := got.Prices[got.Model]; priced {
		t.Fatal("Ollama cloud profile inherited a numeric price")
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
			if _, err := r.Prepare(context.Background(), overrides); err == nil {
				t.Fatal("Prepare accepted an invalid primary route")
			}
		})
	}
}
