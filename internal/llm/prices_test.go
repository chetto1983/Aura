package llm_test

import (
	"math"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// The published rate for deepseek/deepseek-v4-flash on 2026-07-29, per 1M tokens.
var v4flash = llm.Price{InputPer1M: 0.14, OutputPer1M: 0.28, CacheReadPer1M: 0.028}

func TestCost(t *testing.T) {
	prices := map[string]llm.Price{"deepseek/deepseek-v4-flash:nitro": v4flash}

	t.Run("provider_cost_present_returns_exact_usd", func(t *testing.T) {
		got, ok := llm.CostUSD("", prices, "any/model", llm.Usage{
			PromptTokens: 1000, CompletionTokens: 500, Cost: new(0.001234),
		})
		if !ok {
			t.Fatal("ok = false, want true when the provider reported a cost")
		}
		if got != "$0.001234" {
			t.Errorf("display = %q, want $0.001234", got)
		}
	})

	t.Run("provider_cost_absent_resolved_model_uses_rate", func(t *testing.T) {
		// 1M prompt @ 0.14 + 1M completion @ 0.28, no cache reads.
		got, ok := llm.CostUSD("", prices, "deepseek/deepseek-v4-flash:nitro", llm.Usage{
			PromptTokens: 1_000_000, CompletionTokens: 1_000_000,
		})
		if !ok {
			t.Fatal("ok = false, want true for a resolved model")
		}
		if got != "$0.420000" {
			t.Errorf("display = %q, want $0.420000", got)
		}
	})

	t.Run("unresolved_model_reports_na_not_zero", func(t *testing.T) {
		got, ok := llm.CostUSD("", prices, "mystery/model", llm.Usage{PromptTokens: 1000, CompletionTokens: 1000})
		if ok {
			t.Error("ok = true, want false for an unresolved model")
		}
		if got != "n/a" {
			t.Errorf("display = %q, want n/a", got)
		}
		// D-23: an unresolved model must NEVER be reported as $0 / 0.
		if strings.Contains(got, "0") || strings.Contains(got, "$") {
			t.Errorf("display = %q must not look like a dollar amount", got)
		}
	})
}

// The 30-day shape measured on the live account: 54.86M prompt of which 40.55M were
// cache reads, 0.64M completion, real charge $2.889. This pins the reason CacheReadPer1M
// exists — the same tokens priced without it land at $7.86, a 2.7x over-report.
func TestTurnCostUSDChargesCacheReadsAtTheCacheRate(t *testing.T) {
	const (
		prompt     = 54_863_096
		cached     = 40_546_101
		completion = 637_173
	)
	usage := llm.Usage{PromptTokens: prompt, CachedTokens: cached, CompletionTokens: completion}

	withCache, ok := llm.TurnCostUSD("", map[string]llm.Price{"m": v4flash}, "m", usage)
	if !ok {
		t.Fatal("ok = false for a resolved model")
	}
	if math.Abs(withCache-3.318079) > 0.000_01 {
		t.Errorf("usd = %v, want ~3.318079 ((prompt-cached)*0.14 + cached*0.028 + completion*0.28)/1e6", withCache)
	}

	blind := llm.Price{InputPer1M: 0.14, OutputPer1M: 0.28} // no published cache rate
	without, _ := llm.TurnCostUSD("", map[string]llm.Price{"m": blind}, "m", usage)
	if math.Abs(without-7.859242) > 0.000_01 {
		t.Errorf("usd = %v, want ~7.859242 when cache reads bill at the full input rate", without)
	}
	if without <= withCache*2 {
		t.Errorf("the cache-blind figure (%v) should be >2x the cache-aware one (%v) on this "+
			"measured mix; if not, the arithmetic no longer models the real charge", without, withCache)
	}
}

// cached_tokens is a SUBSET of prompt_tokens, never an addend. A count exceeding the
// prompt (or a negative one) must not produce a negative fresh-token charge.
func TestTurnCostUSDClampsCachedTokens(t *testing.T) {
	prices := map[string]llm.Price{"m": v4flash}

	over, ok := llm.TurnCostUSD("", prices, "m", llm.Usage{PromptTokens: 100, CachedTokens: 900})
	if !ok {
		t.Fatal("ok = false for a resolved model")
	}
	want := 100 * 0.028 / 1_000_000 // all 100 prompt tokens treated as cache reads
	if math.Abs(over-want) > 1e-12 {
		t.Errorf("usd = %v, want %v — cached above prompt must clamp to prompt", over, want)
	}

	neg, _ := llm.TurnCostUSD("", prices, "m", llm.Usage{PromptTokens: 100, CachedTokens: -5})
	if neg < 0 {
		t.Errorf("usd = %v, want a non-negative charge for a negative cached count", neg)
	}
}

func TestTurnCostUSD(t *testing.T) {
	prices := map[string]llm.Price{"deepseek/deepseek-v4-flash:nitro": v4flash}

	t.Run("provider_cost_present_wins", func(t *testing.T) {
		got, ok := llm.TurnCostUSD("", prices, "any/model", llm.Usage{
			PromptTokens: 1000, CompletionTokens: 500, Cost: new(0.001234),
		})
		if !ok || got != 0.001234 {
			t.Fatalf("got (%v,%v), want (0.001234,true)", got, ok)
		}
	})

	t.Run("unresolved_model_zero_not_ok", func(t *testing.T) {
		got, ok := llm.TurnCostUSD("", prices, "mystery/model", llm.Usage{PromptTokens: 1000, CompletionTokens: 1000})
		if ok {
			t.Error("ok = true, want false for an unresolved model (caller must not store a fabricated $0)")
		}
		if got != 0 {
			t.Errorf("usd = %v, want 0 sentinel for the not-ok case", got)
		}
	})
}

func TestPriceForSwitchesAtTheThreshold(t *testing.T) {
	long := &llm.LongPromptPrice{MinPromptTokens: 100_000, InputPer1M: 2, OutputPer1M: 10, CacheReadPer1M: 0.2, CacheWritePer1M: 2.5}
	short := llm.Price{InputPer1M: 1, OutputPer1M: 5, CacheReadPer1M: 0.1, CacheWritePer1M: 1.25, LongPrompt: long}
	if got := short.For(99_999); got != short {
		t.Fatalf("below the threshold: %+v", got)
	}
	want := llm.Price{InputPer1M: 2, OutputPer1M: 10, CacheReadPer1M: 0.2, CacheWritePer1M: 2.5}
	for _, prompt := range []int{100_000, 400_000} {
		if got := short.For(prompt); got != want {
			t.Fatalf("at %d: %+v", prompt, got)
		}
	}
	flat := llm.Price{InputPer1M: 3, OutputPer1M: 15}
	if got := flat.For(1_000_000); got != flat {
		t.Fatalf("a flat price changed: %+v", got)
	}
}

// The Claude client prices every call itself, cache writes included, and reports no cost
// only for a call with no honest number. A turn none of whose calls was priced is therefore
// unknown on that route, never a table guess; every other route keeps the table fallback.
func TestTurnCostUSDNeverPricesAnUnpricedClaudeTurn(t *testing.T) {
	prices := map[string]llm.Price{"m": {InputPer1M: 2, OutputPer1M: 10}}
	tokens := llm.Usage{PromptTokens: 1_000_000, CompletionTokens: 1_000_000}
	if usd, ok := llm.TurnCostUSD(llm.AnthropicProvider, prices, "m", tokens); ok || usd != 0 {
		t.Fatalf("unpriced Claude turn = (%v, %v), want the unknown (0, false)", usd, ok)
	}
	priced := tokens
	priced.Cost = new(0.25)
	if usd, ok := llm.TurnCostUSD(llm.AnthropicProvider, prices, "m", priced); !ok || usd != 0.25 {
		t.Fatalf("priced Claude turn = (%v, %v), want (0.25, true)", usd, ok)
	}
	if usd, ok := llm.TurnCostUSD("openrouter", prices, "m", tokens); !ok || usd != 12 {
		t.Fatalf("unpriced OpenRouter turn = (%v, %v), want the table's (12, true)", usd, ok)
	}
	if display, ok := llm.CostUSD(llm.AnthropicProvider, prices, "m", tokens); ok || display != "n/a" {
		t.Fatalf("unpriced Claude display = %q, %v, want n/a", display, ok)
	}
}
