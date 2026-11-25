package llm_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestBaseModelID(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"deepseek/deepseek-v4-flash:nitro", "deepseek/deepseek-v4-flash"},
		{"deepseek/deepseek-v4-flash:exacto", "deepseek/deepseek-v4-flash"},
		{"deepseek/deepseek-v4-flash", "deepseek/deepseek-v4-flash"},
		{"", ""},
	} {
		if got := llm.BaseModelID(tc.in); got != tc.want {
			t.Errorf("BaseModelID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestResolveModelProfileUsesProviderMetadataAndPreservesOverrides(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{
			"id":"deepseek/deepseek-v4-flash",
			"context_length":131072,
			"top_provider":{"max_completion_tokens":16384},
			"pricing":{"prompt":"0.00000014","completion":"0.00000028","input_cache_read":"0.000000028"}
		}]}`))
	}))
	defer srv.Close()

	cfg := llm.Config{
		Provider:                  "openrouter",
		BaseURL:                   srv.URL,
		Model:                     "deepseek/deepseek-v4-flash:nitro",
		ContextWindow:             200000,
		ContextWindowConfigured:   true,
		MaxOutputTokens:           32768,
		MaxTokens:                 4096,
		TotalTimeoutSec:           120,
		MaxOutputTokensConfigured: false,
	}
	if err := cfg.ResolveModelProfile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if cfg.ContextWindow != 200000 {
		t.Fatalf("explicit context = %d, want 200000", cfg.ContextWindow)
	}
	if cfg.MaxOutputTokens != 16384 {
		t.Fatalf("discovered max output = %d, want 16384", cfg.MaxOutputTokens)
	}
	if got := cfg.Prices[cfg.Model]; got != (llm.Price{InputPer1M: 0.14, OutputPer1M: 0.28, CacheReadPer1M: 0.028}) {
		t.Fatalf("resolved price = %+v", got)
	}
}

// An operator-pinned rate (~/.aura/llm.json `prices`) outranks the catalogue: the
// profile fills a price only when the configured model has none.
func TestResolveModelProfileDoesNotClobberAnOperatorPrice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{
			"id":"deepseek/deepseek-v4-flash",
			"context_length":131072,
			"pricing":{"prompt":"0.00000014","completion":"0.00000028"}
		}]}`))
	}))
	defer srv.Close()

	pinned := llm.Price{InputPer1M: 999, OutputPer1M: 999}
	cfg := llm.Config{
		Provider:        "openrouter",
		BaseURL:         srv.URL,
		Model:           "deepseek/deepseek-v4-flash:nitro",
		MaxTokens:       4096,
		TotalTimeoutSec: 120,
		Prices:          map[string]llm.Price{"deepseek/deepseek-v4-flash:nitro": pinned},
	}
	if err := cfg.ResolveModelProfile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Prices["deepseek/deepseek-v4-flash:nitro"]; got != pinned {
		t.Fatalf("price = %+v, want the pinned override %+v", got, pinned)
	}
}

func TestFetchModelProfileDoesNotSendCloudCredentialToLocalProvider(t *testing.T) {
	var seenAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":[{"id":"local","meta":{"n_ctx":81920}}]}`))
	}))
	defer srv.Close()

	if _, err := llm.FetchModelProfile(
		context.Background(), srv.Client(), "llamacpp", srv.URL, "cloud-secret", "local",
	); err != nil {
		t.Fatal(err)
	}
	if seenAuth != "" {
		t.Fatal("local model metadata request received a cloud authorization credential")
	}
}

func TestResolveOllamaCloudProfileUsesShowWithoutCloudCredentialOrPrice(t *testing.T) {
	var seenAuth string
	var seenModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/show" {
			http.NotFound(w, r)
			return
		}
		seenAuth = r.Header.Get("Authorization")
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		seenModel = body.Model
		_, _ = w.Write([]byte(`{"model_info":{"gemma4.context_length":262144}}`))
	}))
	defer srv.Close()

	cfg := llm.Config{
		Provider:        "ollama",
		BaseURL:         srv.URL + "/v1",
		Model:           "glm-5.3:cloud",
		APIKey:          "retained-openrouter-secret",
		ContextWindow:   1_000_000,
		MaxOutputTokens: 32768,
		MaxTokens:       4096,
		TotalTimeoutSec: 120,
		Prices: map[string]llm.Price{
			"glm-5.3:cloud": {InputPer1M: 99, OutputPer1M: 99},
		},
	}
	if err := cfg.ResolveModelProfile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if seenAuth != "" {
		t.Fatal("Ollama model metadata request received a retained cloud credential")
	}
	if seenModel != "glm-5.3:cloud" {
		t.Fatalf("show model = %q", seenModel)
	}
	if cfg.ContextWindow != 262144 || cfg.MaxOutputTokens != 32768 {
		t.Fatalf("resolved Ollama limits = context %d, output %d", cfg.ContextWindow, cfg.MaxOutputTokens)
	}
	if _, priced := cfg.Prices[cfg.Model]; priced {
		t.Fatalf("Ollama cloud inherited a numeric price: %+v", cfg.Prices[cfg.Model])
	}
	if cfg.CostStatus != llm.CostStatusSubscriptionIncluded {
		t.Fatalf("cost status = %q, want subscription-included", cfg.CostStatus)
	}
}

func TestResolveOllamaLocalProfileUsesIncludedZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/show" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"model_info":{"gemma3.context_length":131072}}`))
	}))
	defer srv.Close()

	cfg := llm.Config{
		Provider: "ollama", BaseURL: srv.URL + "/v1", Model: "gemma3:27b",
		ContextWindow: 1_000_000, MaxOutputTokens: 32768, MaxTokens: 4096, TotalTimeoutSec: 120,
	}
	if err := cfg.ResolveModelProfile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if cfg.CostStatus != llm.CostStatusLocalIncluded {
		t.Fatalf("cost status = %q, want local-included", cfg.CostStatus)
	}
	if price, ok := cfg.Prices[cfg.Model]; !ok || price != (llm.Price{}) {
		t.Fatalf("local Ollama price = (%+v,%v), want explicit included zero", price, ok)
	}
}

func TestResolveOllamaProfileRejectsAmbiguousContextWithoutMutation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"model_info":{"first.context_length":8192,"second.context_length":16384}}`))
	}))
	defer srv.Close()

	cfg := llm.Config{
		Provider: "ollama", BaseURL: srv.URL + "/v1", Model: "ambiguous",
		ContextWindow: 999, MaxOutputTokens: 100, MaxTokens: 50, TotalTimeoutSec: 120,
	}
	err := cfg.ResolveModelProfile(context.Background())
	if !errors.Is(err, llm.ErrModelProfileUnavailable) {
		t.Fatalf("err = %v, want ErrModelProfileUnavailable", err)
	}
	if cfg.ContextWindow != 999 || cfg.CostStatus != "" || len(cfg.Prices) != 0 {
		t.Fatalf("failed Ollama resolution mutated config: %+v", cfg)
	}
}

func TestResolveModelProfileRejectsMissingContextWithoutMutatingConfig(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"local","meta":{}}]}`))
	}))
	defer srv.Close()

	cfg := llm.Config{Provider: "llamacpp", BaseURL: srv.URL, Model: "local", ContextWindow: 999}
	err := cfg.ResolveModelProfile(context.Background())
	if !errors.Is(err, llm.ErrModelProfileUnavailable) {
		t.Fatalf("err = %v, want ErrModelProfileUnavailable", err)
	}
	if cfg.ContextWindow != 999 || len(cfg.Prices) != 0 {
		t.Fatalf("failed resolution mutated config: %+v", cfg)
	}
}
