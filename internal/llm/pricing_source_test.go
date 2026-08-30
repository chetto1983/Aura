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

// The payload shape verified against the live endpoint on 2026-07-29: rates are JSON
// STRINGS, and input_cache_read is ABSENT (not null) for 155 of the 367 models.
const modelsPayload = `{"data":[
  {"id":"openai/gpt-4o","pricing":{"prompt":"0.0000025","completion":"0.00001"}},
  {"id":"deepseek/deepseek-v4-flash","pricing":{"prompt":"0.00000014","completion":"0.00000028","input_cache_read":"0.000000028"}},
  {"id":"cohere/rerank-4-pro","pricing":{"prompt":"0","completion":"0"}}
]}`

func modelsServer(t *testing.T, seenAuth *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		if seenAuth != nil {
			*seenAuth = r.Header.Get("Authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(modelsPayload))
	}))
	t.Cleanup(srv.Close)
	return srv
}

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

func TestFetchModelPriceResolvesTheRoutingVariant(t *testing.T) {
	srv := modelsServer(t, nil)

	// The configured id carries `:nitro`; the catalogue publishes only the base id, so
	// a lookup that skips normalisation misses every time.
	got, err := llm.FetchModelPrice(context.Background(), srv.Client(), srv.URL, "k",
		"deepseek/deepseek-v4-flash:nitro")
	if err != nil {
		t.Fatalf("FetchModelPrice: %v", err)
	}
	want := llm.Price{InputPer1M: 0.14, OutputPer1M: 0.28, CacheReadPer1M: 0.028}
	if got != want {
		t.Errorf("price = %+v, want %+v (per-token strings scaled by 1e6)", got, want)
	}
}

func TestFetchModelPriceLeavesCacheRateZeroWhenUnpublished(t *testing.T) {
	srv := modelsServer(t, nil)

	got, err := llm.FetchModelPrice(context.Background(), srv.Client(), srv.URL, "", "openai/gpt-4o")
	if err != nil {
		t.Fatalf("FetchModelPrice: %v", err)
	}
	if got.CacheReadPer1M != 0 {
		t.Errorf("CacheReadPer1M = %v, want 0 when the model omits input_cache_read", got.CacheReadPer1M)
	}
	// CostUSDValue reads that zero as "bill cache reads at the input rate".
	if got.InputPer1M != 2.5 || got.OutputPer1M != 10 {
		t.Errorf("price = %+v, want 2.5/10 per 1M", got)
	}
}

func TestFetchModelPriceOmitsAuthorizationWhenNoKey(t *testing.T) {
	var seen string
	srv := modelsServer(t, &seen)

	// GET /models needs no credential — verified live (200 with no header, and with a
	// garbage bearer). An empty key must therefore not be an error.
	if _, err := llm.FetchModelPrice(context.Background(), srv.Client(), srv.URL, "", "openai/gpt-4o"); err != nil {
		t.Fatalf("FetchModelPrice with an empty key: %v", err)
	}
	if seen != "" {
		t.Errorf("Authorization = %q, want it unset when no key is configured", seen)
	}

	if _, err := llm.FetchModelPrice(context.Background(), srv.Client(), srv.URL, "k", "openai/gpt-4o"); err != nil {
		t.Fatalf("FetchModelPrice with a key: %v", err)
	}
	if seen != "Bearer k" {
		t.Errorf("Authorization = %q, want %q", seen, "Bearer k")
	}
}

func TestFetchModelPriceUnavailableCases(t *testing.T) {
	srv := modelsServer(t, nil)

	t.Run("model_absent_from_catalogue", func(t *testing.T) {
		_, err := llm.FetchModelPrice(context.Background(), srv.Client(), srv.URL, "k", "who/knows")
		if !errors.Is(err, llm.ErrPricingUnavailable) {
			t.Errorf("err = %v, want ErrPricingUnavailable", err)
		}
	})

	t.Run("empty_model_id", func(t *testing.T) {
		_, err := llm.FetchModelPrice(context.Background(), srv.Client(), srv.URL, "k", "")
		if !errors.Is(err, llm.ErrPricingUnavailable) {
			t.Errorf("err = %v, want ErrPricingUnavailable", err)
		}
	})

	t.Run("non_200", func(t *testing.T) {
		down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer down.Close()
		_, err := llm.FetchModelPrice(context.Background(), down.Client(), down.URL, "k", "openai/gpt-4o")
		if !errors.Is(err, llm.ErrPricingUnavailable) {
			t.Errorf("err = %v, want ErrPricingUnavailable", err)
		}
	})
}

// A local backend publishes no catalogue and costs nothing per token, so resolution is
// skipped rather than attempted — and must not dial anywhere to discover that.
func TestResolvePricingSkipsLocalBackend(t *testing.T) {
	cfg := llm.Config{
		Provider: "llamacpp",
		BaseURL:  "http://127.0.0.1:1/v1", // a port nothing listens on: a dial would fail loudly
		Model:    "local/whatever",
	}
	err := cfg.ResolvePricing(context.Background())
	if !errors.Is(err, llm.ErrPricingNotApplicable) {
		t.Errorf("err = %v, want ErrPricingNotApplicable for a local backend", err)
	}
	// The two sentinels must stay distinct: boot warns on Unavailable (something
	// readable was not read) and stays quiet on NotApplicable (nothing to read).
	if errors.Is(err, llm.ErrPricingUnavailable) {
		t.Error("a local backend must not report ErrPricingUnavailable — it would warn on every local boot")
	}
	if len(cfg.Prices) != 0 {
		t.Errorf("Prices = %v, want untouched for a local backend", cfg.Prices)
	}
}

// ~/.aura/llm.json `prices` stays the operator's final word (A3, unchanged by #93), so
// a fetched rate must never overwrite an entry the operator pinned.
func TestResolvePricingDoesNotClobberAnOperatorOverride(t *testing.T) {
	srv := modelsServer(t, nil)
	pinned := llm.Price{InputPer1M: 999, OutputPer1M: 999}
	cfg := llm.Config{
		BaseURL: srv.URL,
		Model:   "deepseek/deepseek-v4-flash:nitro",
		Prices:  map[string]llm.Price{"deepseek/deepseek-v4-flash:nitro": pinned},
	}

	if err := cfg.ResolvePricing(context.Background()); err != nil {
		t.Fatalf("ResolvePricing: %v", err)
	}
	if got := cfg.Prices["deepseek/deepseek-v4-flash:nitro"]; got != pinned {
		t.Errorf("price = %+v, want the pinned override %+v", got, pinned)
	}
}

// The resolved rate is keyed on the CONFIGURED id, because that is the string every
// CostUSD call site passes (cfg.Model) — not the base id the catalogue publishes.
func TestResolvePricingKeysOnTheConfiguredID(t *testing.T) {
	srv := modelsServer(t, nil)
	cfg := llm.Config{BaseURL: srv.URL, Model: "deepseek/deepseek-v4-flash:nitro"}

	if err := cfg.ResolvePricing(context.Background()); err != nil {
		t.Fatalf("ResolvePricing: %v", err)
	}
	if _, ok := cfg.Prices["deepseek/deepseek-v4-flash:nitro"]; !ok {
		t.Fatalf("Prices keyed %v, want an entry under the configured id", cfg.Prices)
	}
	usd, ok := llm.CostUSD(cfg.Prices, cfg.Model, llm.Usage{PromptTokens: 1_000_000})
	if !ok || usd != "$0.140000" {
		t.Errorf("CostUSD = (%q,%v), want ($0.140000,true) straight off the resolved map", usd, ok)
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
		Model:           "gemma4:31b-cloud",
		APIKey:          "retained-openrouter-secret",
		ContextWindow:   1_000_000,
		MaxOutputTokens: 32768,
		MaxTokens:       4096,
		TotalTimeoutSec: 120,
		Prices: map[string]llm.Price{
			"gemma4:31b-cloud": {InputPer1M: 99, OutputPer1M: 99},
		},
	}
	if err := cfg.ResolveModelProfile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if seenAuth != "" {
		t.Fatal("Ollama model metadata request received a retained cloud credential")
	}
	if seenModel != "gemma4:31b-cloud" {
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
