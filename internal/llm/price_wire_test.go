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
			if (got.LongPrompt == nil) != (tc.long == nil) {
				t.Fatalf("LongPrompt = %+v, want %+v", got.LongPrompt, tc.long)
			}
			if tc.long != nil {
				l := got.LongPrompt
				if l.MinPromptTokens != tc.long.MinPromptTokens || !near(l.InputPer1M, tc.long.InputPer1M) ||
					!near(l.OutputPer1M, tc.long.OutputPer1M) || !near(l.CacheReadPer1M, tc.long.CacheReadPer1M) ||
					!near(l.CacheWritePer1M, tc.long.CacheWritePer1M) {
					t.Fatalf("LongPrompt = %+v, want %+v", l, tc.long)
				}
			}
		})
	}
}
