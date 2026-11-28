package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/anthropic"
)

const testHouseKey = "sk-ant-api03-house-key-for-cmd-tests-0123456789"

// claudePricesWire is OpenRouter's public list for two Claude models (spec §5.7, measured
// 2026-11-27): Haiku carries the long-prompt override, Sonnet does not.
const claudePricesWire = `{"data":[
 {"id":"anthropic/claude-haiku-5.5","context_length":1000000,
  "pricing":{"prompt":"0.0000001","completion":"0.0000005","input_cache_read":"0.00000001","input_cache_write":"0.000000125",
   "overrides":[{"min_prompt_tokens":100000,"prompt":"0.0000005","completion":"0.0000025",
    "input_cache_read":"0.00000005","input_cache_write":"0.000000625"}]}},
 {"id":"anthropic/claude-sonnet-5.5","context_length":1000000,
  "pricing":{"prompt":"0.000002","completion":"0.00001","input_cache_read":"0.0000001","input_cache_write":"0.0000025"}}]}`

func claudeModels() []llm.ModelCatalogEntry {
	every := []llm.ReasoningEffort{llm.ReasoningEffortLow, llm.ReasoningEffortMedium, llm.ReasoningEffortHigh, llm.ReasoningEffortXHigh, llm.ReasoningEffortMax}
	return []llm.ModelCatalogEntry{
		{ID: "claude-sonnet-5-5", ContextWindow: 1_000_000, MaxOutputTokens: 128_000, SupportedReasoningEfforts: every, ReasoningMandatory: true},
		{ID: "claude-haiku-5-5", ContextWindow: 1_000_000, MaxOutputTokens: 128_000, SupportedReasoningEfforts: every[:3]},
	}
}

func houseKeySource(context.Context) (string, error) { return testHouseKey, nil }

// stubClaudeCatalog replaces the account catalogue with models, asserting the house key is
// what it is handed, and points the list-price lookup at a fixture that refuses any
// credential. It returns the number of price requests seen.
func stubClaudeCatalog(t *testing.T, models []llm.ModelCatalogEntry) *int {
	t.Helper()
	priceHits := new(int)
	prices := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*priceHits++
		if r.Header.Get("Authorization") != "" || r.URL.Path != "/models" {
			t.Errorf("price lookup %s carried a credential or the wrong path", r.URL.Path)
		}
		_, _ = w.Write([]byte(claudePricesWire))
	}))
	t.Cleanup(prices.Close)
	oldFetch, oldURL := fetchAnthropicModels, anthropicPriceCatalogURL
	fetchAnthropicModels = func(ctx context.Context, key anthropic.KeySource) ([]llm.ModelCatalogEntry, error) {
		value, err := key(ctx)
		if err != nil || value != testHouseKey {
			t.Errorf("catalogue key %q, %v: want the house key", value, err)
		}
		return models, nil
	}
	anthropicPriceCatalogURL = prices.URL
	t.Cleanup(func() { fetchAnthropicModels, anthropicPriceCatalogURL = oldFetch, oldURL })
	return priceHits
}

func claudeOverrides(model string) map[string]string {
	return map[string]string{"AURA_LLM_PROVIDER": llm.AnthropicProvider, "AURA_LLM_BASE_URL": llm.AnthropicBaseURL, "AURA_LLM_MODEL": model}
}

func TestRouteValidationAcceptsOnlyTheAnthropicEndpoint(t *testing.T) {
	r := &primaryLLMRouteReloader{fallback: validFallbackLLMConfig()}
	if _, err := r.resolve(claudeOverrides("claude-sonnet-5-5"), nil); err != nil {
		t.Fatalf("the official endpoint was refused: %v", err)
	}
	for _, baseURL := range []string{"https://attacker.example", "http://api.anthropic.com", "https://api.anthropic.com/v1", "https://api.anthropic.com.attacker.example"} {
		overrides := claudeOverrides("claude-sonnet-5-5")
		overrides["AURA_LLM_BASE_URL"] = baseURL
		if _, err := r.resolve(overrides, nil); err == nil {
			t.Fatalf("the house key could be sent to %s", baseURL)
		}
	}
}

func TestAnthropicRouteSwitchesLiveWithoutTheServicesKey(t *testing.T) {
	priceHits := stubClaudeCatalog(t, claudeModels())
	fallback := validFallbackLLMConfig()
	fallback.APIKey = "sk-or-v1-boot-services-key"
	runtime := llm.NewRuntime(&settingsRuntimeClient{route: "old"}, fallback)
	r := &primaryLLMRouteReloader{fallback: fallback, runtime: runtime, clients: llmClientDeps{anthropicKey: houseKeySource}}
	overrides := claudeOverrides("claude-sonnet-5-5")
	overrides["OPENROUTER_API_KEY"] = "sk-or-v1-services-key-override"
	apply, err := r.Prepare(context.Background(), overrides, []string{"AURA_MODEL_CONTEXT_WINDOW", "AURA_MODEL_MAX_OUTPUT_TOKENS"})
	if err != nil {
		t.Fatal(err)
	}
	apply()
	snapshot := runtime.Snapshot()
	got := snapshot.Config
	if got.Provider != llm.AnthropicProvider || got.APIKey != "" || len(got.Headers) != 0 {
		t.Fatalf("published route %q with key %q headers %v", got.Provider, got.APIKey, got.Headers)
	}
	if _, ok := snapshot.Client.(anthropicRouteClient); !ok {
		t.Fatalf("published client %T, want the Claude route client", snapshot.Client)
	}
	if got.ContextWindow != 200_000 || got.MaxTokens != 32_768 || got.MaxOutputTokens != 32_768 {
		t.Fatalf("limits window %d max %d cap %d", got.ContextWindow, got.MaxTokens, got.MaxOutputTokens)
	}
	if price, ok := got.Prices["claude-sonnet-5-5"]; !ok || price.InputPer1M != 2 || price.CacheWritePer1M != 2.5 || got.CostStatus != llm.CostStatusRateEstimate {
		t.Fatalf("price %+v status %q", got.Prices["claude-sonnet-5-5"], got.CostStatus)
	}
	if *priceHits != 1 {
		t.Fatalf("price lookups %d, want 1", *priceHits)
	}
}

func TestNoHouseKeyRefusesWithoutDialing(t *testing.T) {
	cfg := llm.Config{Provider: llm.AnthropicProvider, BaseURL: llm.AnthropicBaseURL, Model: "claude-sonnet-5-5", APIKey: "sk-or-v1-services-key"}
	for name, key := range map[string]anthropic.KeySource{
		"no key source": nil,
		"empty key":     func(context.Context) (string, error) { return " ", nil },
	} {
		t.Run(name, func(t *testing.T) {
			stream, err := newLLMClient(cfg, llmClientDeps{anthropicKey: key}).Stream(context.Background(), llm.Request{})
			if stream != nil || !errors.As(err, new(llmNotConfiguredError)) {
				t.Fatalf("stream %v, error %v: want the not-configured refusal", stream, err)
			}
			var payload struct{ Error, Hint string }
			if json.Unmarshal([]byte(err.Error()), &payload) != nil || payload.Error != llmNotConfiguredCode || payload.Hint != "connect a model in Settings, then retry" {
				t.Fatalf("refusal %q", err.Error())
			}
		})
	}
	storeDown := errors.New("settings store unavailable")
	_, err := newLLMClient(cfg, llmClientDeps{anthropicKey: func(context.Context) (string, error) { return "", storeDown }}).Stream(context.Background(), llm.Request{})
	if !errors.Is(err, storeDown) || errors.As(err, new(llmNotConfiguredError)) {
		t.Fatalf("a store failure became %v", err)
	}
}

func TestAnthropicRouteNeedsNoIdentityKeys(t *testing.T) {
	runtime := llm.NewRuntime(nil, llm.Config{Provider: llm.AnthropicProvider, BaseURL: llm.AnthropicBaseURL})
	bills := liveRouteBills(&chatEnv{llmRuntime: runtime})
	if bills() {
		t.Fatal("the Claude route triggers OpenRouter key minting")
	}
	runtime.Replace(nil, llm.Config{Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1"})
	if !bills() {
		t.Fatal("switching back lost the OpenRouter credit policy")
	}
}

func claudeProfile(t *testing.T, model string) llm.Config {
	t.Helper()
	cfg := validFallbackLLMConfig()
	cfg.Provider, cfg.BaseURL, cfg.Model = llm.AnthropicProvider, llm.AnthropicBaseURL, model
	if err := cfg.ResolveAnthropicProfile(); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestValidateAnthropicModelSetsTheWorkingWindow(t *testing.T) {
	haikuPrice := llm.Price{InputPer1M: 0.1, OutputPer1M: 0.5, LongPrompt: &llm.LongPromptPrice{MinPromptTokens: 100_000, InputPer1M: 0.5, OutputPer1M: 2.5}}
	haiku := claudeProfile(t, "claude-haiku-5-5")
	if err := applyAnthropicCatalogModel(&haiku, claudeModels(), haikuPrice, true); err != nil {
		t.Fatal(err)
	}
	if haiku.ContextWindow != 100_000 || haiku.MaxTokens != 32_768 || haiku.MaxOutputTokens != 32_768 || haiku.Validate() != nil {
		t.Fatalf("haiku window %d max %d cap %d: %v", haiku.ContextWindow, haiku.MaxTokens, haiku.MaxOutputTokens, haiku.Validate())
	}
	if haiku.ReasoningMandatory || len(haiku.SupportedReasoningEfforts) != 3 || haiku.CostStatus != llm.CostStatusRateEstimate {
		t.Fatalf("haiku reasoning %v mandatory %v status %q", haiku.SupportedReasoningEfforts, haiku.ReasoningMandatory, haiku.CostStatus)
	}

	sonnet := claudeProfile(t, "claude-sonnet-5-5")
	if err := applyAnthropicCatalogModel(&sonnet, claudeModels(), llm.Price{InputPer1M: 2, OutputPer1M: 10}, true); err != nil {
		t.Fatal(err)
	}
	if sonnet.ContextWindow != 200_000 || !sonnet.ReasoningMandatory || sonnet.Validate() != nil {
		t.Fatalf("sonnet window %d mandatory %v", sonnet.ContextWindow, sonnet.ReasoningMandatory)
	}

	pinned := claudeProfile(t, "claude-haiku-5-5")
	pinned.ContextWindow, pinned.ContextWindowConfigured = 150_000, true
	pinned.MaxOutputTokens, pinned.MaxOutputTokensConfigured = 40_000, true
	if err := applyAnthropicCatalogModel(&pinned, claudeModels(), haikuPrice, true); err != nil {
		t.Fatal(err)
	}
	if pinned.ContextWindow != 150_000 || pinned.MaxOutputTokens != 40_000 {
		t.Fatalf("pins lost: window %d cap %d", pinned.ContextWindow, pinned.MaxOutputTokens)
	}

	narrow := claudeProfile(t, "claude-sonnet-5-5")
	models := claudeModels()
	models[0].ContextWindow = 150_000
	if err := applyAnthropicCatalogModel(&narrow, models, llm.Price{}, false); err != nil {
		t.Fatal(err)
	}
	if narrow.ContextWindow != 150_000 || narrow.CostStatus != llm.CostStatusUnknown {
		t.Fatalf("window %d above max_input_tokens or a cost status %q with no price", narrow.ContextWindow, narrow.CostStatus)
	}
	if _, priced := narrow.Prices["claude-sonnet-5-5"]; priced {
		t.Fatal("a price was invented")
	}

	if applyAnthropicCatalogModel(&narrow, claudeModels()[1:], llm.Price{}, false) == nil {
		t.Fatal("accepted a model the account does not list")
	}
	models[0].SupportedReasoningEfforts[0] = llm.ReasoningEffortNone
	if slices.Contains(narrow.SupportedReasoningEfforts, llm.ReasoningEffortNone) {
		t.Fatal("the catalogue's slice aliases the route snapshot")
	}
}

// MaxTokens has no pin flag: below the client's 16,384 floor (the generic 4,096 default
// included) it becomes 32,768; from the floor up it is the operator's and is kept.
func TestAnthropicMaxTokensFloorAndPin(t *testing.T) {
	for _, tc := range []struct{ configured, want int }{{4_096, 32_768}, {16_383, 32_768}, {16_384, 16_384}, {20_000, 20_000}} {
		cfg := claudeProfile(t, "claude-sonnet-5-5")
		cfg.MaxTokens = tc.configured
		if err := applyAnthropicCatalogModel(&cfg, claudeModels(), llm.Price{}, false); err != nil {
			t.Fatal(err)
		}
		if cfg.MaxTokens != tc.want || cfg.MaxOutputTokens != tc.want {
			t.Fatalf("MaxTokens %d -> max %d cap %d, want %d", tc.configured, cfg.MaxTokens, cfg.MaxOutputTokens, tc.want)
		}
	}
	above := claudeProfile(t, "claude-sonnet-5-5")
	above.MaxTokens = 200_000
	if applyAnthropicCatalogModel(&above, claudeModels(), llm.Price{}, false) == nil {
		t.Fatal("accepted max_tokens above what the model allows")
	}
}

func TestValidateAnthropicModelNeedsTheHouseKey(t *testing.T) {
	if validateAnthropicModel(context.Background(), &llm.Config{Provider: "openrouter"}, nil) != nil {
		t.Fatal("OpenRouter required the Claude key")
	}
	cfg := claudeProfile(t, "claude-sonnet-5-5")
	before := cfg
	err := validateAnthropicModel(context.Background(), &cfg, func(context.Context) (string, error) { return "", nil })
	if err == nil || !strings.Contains(err.Error(), "Settings") || cfg.ContextWindow != before.ContextWindow {
		t.Fatalf("no key: %v, window %d", err, cfg.ContextWindow)
	}
}

func TestBootResolvesTheClaudeProfileFailSoft(t *testing.T) {
	stubClaudeCatalog(t, claudeModels())
	cfg := claudeProfile(t, "claude-haiku-5-5")
	bootAnthropicProfile(context.Background(), &cfg, houseKeySource)
	if cfg.ContextWindow != 100_000 || cfg.Prices["claude-haiku-5-5"].LongPrompt == nil {
		t.Fatalf("boot window %d price %+v", cfg.ContextWindow, cfg.Prices["claude-haiku-5-5"])
	}
	unlisted := claudeProfile(t, "claude-unlisted-5-5")
	before := unlisted.ContextWindow
	bootAnthropicProfile(context.Background(), &unlisted, houseKeySource)
	if unlisted.ContextWindow != before {
		t.Fatal("a failed boot validation changed the fallback")
	}
}

// A pinned output cap holds the unpinned MaxTokens default down to it; one below the floor
// cannot carry adaptive thinking and is refused by name, at save and in the boot log.
func TestPinnedOutputCapBoundsTheClaudeDefault(t *testing.T) {
	pinned := func(capTokens int) llm.Config {
		cfg := claudeProfile(t, "claude-sonnet-5-5")
		cfg.MaxOutputTokens, cfg.MaxOutputTokensConfigured = capTokens, true
		return cfg
	}
	held := pinned(20_000)
	if err := applyAnthropicCatalogModel(&held, claudeModels(), llm.Price{}, false); err != nil {
		t.Fatal(err)
	}
	if held.MaxTokens != 20_000 || held.MaxOutputTokens != 20_000 {
		t.Fatalf("max %d cap %d, want both at the pinned 20000", held.MaxTokens, held.MaxOutputTokens)
	}

	low := pinned(8_000)
	err := applyAnthropicCatalogModel(&low, claudeModels(), llm.Price{}, false)
	const refusal = "max output tokens pinned at 8000 is below the Claude route's 16384 floor"
	if err == nil || err.Error() != refusal {
		t.Fatalf("error %v, want %q", err, refusal)
	}

	stubClaudeCatalog(t, claudeModels())
	var logs safeBuffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	boot := pinned(8_000)
	bootAnthropicProfile(context.Background(), &boot, houseKeySource)
	if !strings.Contains(logs.String(), "pinned at 8000 is below the Claude route's 16384 floor") || boot.MaxTokens != low.MaxTokens {
		t.Fatalf("boot log %q, max %d: the refusal must be logged and the fallback kept", logs.String(), boot.MaxTokens)
	}
}

// safeBuffer is a log sink other tests' goroutines may write to while this one reads it.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
