package llm_test

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

const anthropicPricesWire = `{"data":[
 {"id":"anthropic/claude-haiku-5.5","context_length":1000000,
  "pricing":{"prompt":"0.0000001","completion":"0.0000005","input_cache_read":"0.00000001","input_cache_write":"0.000000125",
   "overrides":[{"min_prompt_tokens":100000,"prompt":"0.0000005","completion":"0.0000025",
    "input_cache_read":"0.00000005","input_cache_write":"0.000000625"}]}},
 {"id":"anthropic/claude-sonnet-5.5","context_length":1000000,
  "pricing":{"prompt":"0.000002","completion":"0.00001","input_cache_read":"0.0000001","input_cache_write":"0.0000025"}}]}`

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func samePrice(a, b llm.Price) bool {
	return near(a.InputPer1M, b.InputPer1M) && near(a.OutputPer1M, b.OutputPer1M) &&
		near(a.CacheReadPer1M, b.CacheReadPer1M) && near(a.CacheWritePer1M, b.CacheWritePer1M)
}

func TestOpenRouterWireReadsCacheWriteAndLongPrompt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(anthropicPricesWire))
	}))
	defer srv.Close()
	cfg := llm.Config{Provider: "openrouter", BaseURL: srv.URL, ContextWindow: 200000, MaxOutputTokens: 32768,
		MaxTokens: 4096, TotalTimeoutSec: 120}
	for _, tc := range []struct {
		model string
		want  llm.Price
		long  *llm.LongPromptPrice
	}{
		{"anthropic/claude-haiku-5.5", llm.Price{InputPer1M: 0.1, OutputPer1M: 0.5, CacheReadPer1M: 0.01, CacheWritePer1M: 0.125},
			&llm.LongPromptPrice{MinPromptTokens: 100000, InputPer1M: 0.5, OutputPer1M: 2.5, CacheReadPer1M: 0.05, CacheWritePer1M: 0.625}},
		{"anthropic/claude-sonnet-5.5", llm.Price{InputPer1M: 2, OutputPer1M: 10, CacheReadPer1M: 0.1, CacheWritePer1M: 2.5}, nil},
	} {
		t.Run(tc.model, func(t *testing.T) {
			c := cfg
			c.Model = tc.model
			if err := c.ResolveModelProfile(context.Background()); err != nil {
				t.Fatal(err)
			}
			got := c.Prices[tc.model]
			if !samePrice(got, tc.want) {
				t.Fatalf("price = %+v, want %+v", got, tc.want)
			}
			if !sameLongPrompt(got.LongPrompt, tc.long) {
				t.Fatalf("LongPrompt = %+v, want %+v", got.LongPrompt, tc.long)
			}
		})
	}
}

func sameLongPrompt(a, b *llm.LongPromptPrice) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.MinPromptTokens == b.MinPromptTokens && near(a.InputPer1M, b.InputPer1M) &&
		near(a.OutputPer1M, b.OutputPer1M) && near(a.CacheReadPer1M, b.CacheReadPer1M) &&
		near(a.CacheWritePer1M, b.CacheWritePer1M)
}

func catalogPrice(t *testing.T, body, model string) llm.ModelCatalogEntry {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	entries, err := llm.FetchModelCatalog(context.Background(), srv.Client(), "openrouter", srv.URL, "")
	if err != nil {
		t.Fatalf("FetchModelCatalog: %v", err)
	}
	for _, entry := range entries {
		if entry.ID == model {
			return entry
		}
	}
	t.Fatalf("model %s not in the catalogue %+v", model, entries)
	return llm.ModelCatalogEntry{}
}

// The model picker reads the same wire: cache-write and long-prompt rates reach it too.
func TestModelCatalogCarriesCacheWriteAndLongPrompt(t *testing.T) {
	entry := catalogPrice(t, anthropicPricesWire, "anthropic/claude-haiku-5.5")
	want := llm.Price{InputPer1M: 0.1, OutputPer1M: 0.5, CacheReadPer1M: 0.01, CacheWritePer1M: 0.125}
	if !entry.HasPrice || !samePrice(entry.Price, want) {
		t.Fatalf("price = %+v (has %v), want %+v", entry.Price, entry.HasPrice, want)
	}
	long := &llm.LongPromptPrice{MinPromptTokens: 100000, InputPer1M: 0.5, OutputPer1M: 2.5, CacheReadPer1M: 0.05, CacheWritePer1M: 0.625}
	if !sameLongPrompt(entry.Price.LongPrompt, long) {
		t.Fatalf("LongPrompt = %+v, want %+v", entry.Price.LongPrompt, long)
	}
}

// Shapes measured on GET /models 2026-11-28: qwen/qwen3.7-flash publishes two prompt-size
// tiers, and tencent/hy3 publishes UTC time windows with no min_prompt_tokens. The kept
// tier is the lowest threshold, wherever it sits in the array; a time window is no tier.
func TestLongPromptIsTheLowestSizeTier(t *testing.T) {
	const wire = `{"data":[{"id":"qwen/two-tiers","pricing":{"prompt":"0.00000005","completion":"0.0000002",
  "overrides":[
   {"utc_start":0,"utc_end":1600,"prompt":"0.000000001","completion":"0.000000001"},
   {"min_prompt_tokens":256000,"prompt":"0.0000002","completion":"0.0000008","input_cache_read":"0.00000004","input_cache_write":"0.00000025"},
   {"min_prompt_tokens":32000,"prompt":"0.0000001","completion":"0.0000004","input_cache_read":"0.00000002","input_cache_write":"0.000000125"}]}}]}`
	entry := catalogPrice(t, wire, "qwen/two-tiers")
	want := &llm.LongPromptPrice{MinPromptTokens: 32000, InputPer1M: 0.1, OutputPer1M: 0.4, CacheReadPer1M: 0.02, CacheWritePer1M: 0.125}
	if !sameLongPrompt(entry.Price.LongPrompt, want) {
		t.Fatalf("LongPrompt = %+v, want the 32k tier %+v", entry.Price.LongPrompt, want)
	}
}
