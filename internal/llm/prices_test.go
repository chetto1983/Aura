package llm_test

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestCost(t *testing.T) {
	// A representative price table (mirrors the seeded entry shape).
	prices := map[string]llm.Price{
		"deepseek/deepseek-v4-flash:nitro": {InputPer1M: 0.0983, OutputPer1M: 0.1966},
	}

	t.Run("provider_cost_present_returns_exact_usd", func(t *testing.T) {
		cost := 0.001234
		got, ok := llm.CostUSD(prices, "any/model", 1000, 500, &cost)
		if !ok {
			t.Fatal("ok = false, want true when providerCost is present")
		}
		if got != "$0.001234" {
			t.Errorf("display = %q, want $0.001234", got)
		}
	})

	t.Run("provider_cost_absent_known_model_uses_table", func(t *testing.T) {
		// 1,000,000 input @ 0.0983 + 1,000,000 output @ 0.1966 = 0.2949
		got, ok := llm.CostUSD(prices, "deepseek/deepseek-v4-flash:nitro", 1_000_000, 1_000_000, nil)
		if !ok {
			t.Fatal("ok = false, want true for a known model")
		}
		if got != "$0.294900" {
			t.Errorf("display = %q, want $0.294900", got)
		}
	})

	t.Run("unknown_model_reports_na_not_zero", func(t *testing.T) {
		got, ok := llm.CostUSD(prices, "mystery/model", 1000, 1000, nil)
		if ok {
			t.Error("ok = true, want false for an unknown model")
		}
		if got != "n/a" {
			t.Errorf("display = %q, want n/a", got)
		}
		// D-23: an unknown model must NEVER be reported as $0 / 0.
		if strings.Contains(got, "0") || strings.Contains(got, "$") {
			t.Errorf("display = %q must not look like a dollar amount", got)
		}
	})
}

// TestCostUSDValue covers the numeric core shared by the displayed footer and the
// persisted cost: provider cost wins, the table fills the wire-omitted gap, and an
// unknown model returns ok=false so the caller stores an honest absence (not a fake $0).
func TestCostUSDValue(t *testing.T) {
	prices := map[string]llm.Price{
		"deepseek/deepseek-v4-flash:nitro": {InputPer1M: 0.0983, OutputPer1M: 0.1966},
	}

	t.Run("provider_cost_present_wins", func(t *testing.T) {
		c := 0.001234
		got, ok := llm.CostUSDValue(prices, "any/model", 1000, 500, &c)
		if !ok || got != 0.001234 {
			t.Fatalf("got (%v,%v), want (0.001234,true)", got, ok)
		}
	})

	t.Run("provider_cost_absent_known_model_uses_table", func(t *testing.T) {
		got, ok := llm.CostUSDValue(prices, "deepseek/deepseek-v4-flash:nitro", 1_000_000, 1_000_000, nil)
		if !ok {
			t.Fatal("ok = false, want true for a known model")
		}
		if got < 0.29489 || got > 0.29491 { // 0.0983 + 0.1966 = 0.2949
			t.Errorf("usd = %v, want ~0.2949", got)
		}
	})

	t.Run("unknown_model_zero_not_ok", func(t *testing.T) {
		got, ok := llm.CostUSDValue(prices, "mystery/model", 1000, 1000, nil)
		if ok {
			t.Error("ok = true, want false for an unknown model (caller must not store a fabricated $0)")
		}
		if got != 0 {
			t.Errorf("usd = %v, want 0 sentinel for the not-ok case", got)
		}
	})
}
