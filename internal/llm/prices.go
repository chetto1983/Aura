package llm

import "fmt"

// Price is a per-model fallback rate in USD per 1,000,000 tokens (D-23). It is
// the safety net only — OpenRouter's reported usage.cost is always preferred
// when present (D-18), because it already nets cache reads/writes and inference.
type Price struct {
	InputPer1M  float64 `json:"input_per_1m"`
	OutputPer1M float64 `json:"output_per_1m"`
}

// defaultPrices returns the seeded fallback price table (D-23). Seeded from the
// OpenRouter model page for deepseek/deepseek-v4-flash:nitro at commit time
// (2026-05-30): standard list price $0.0983/1M input, $0.1966/1M output
// (RESEARCH Assumptions A1). ~/.aura/llm.json `prices` overrides entry-by-entry.
// A fresh map is returned per call so a caller's overlay never mutates the seed.
func defaultPrices() map[string]Price {
	return map[string]Price{
		"deepseek/deepseek-v4-flash:nitro": {InputPer1M: 0.0983, OutputPer1M: 0.1966},
	}
}

// CostUSDValue reports the numeric turn cost in USD and an ok flag, with the D-18/D-23
// precedence below. It is the shared core for both the displayed footer (CostUSD) and
// the persisted cost (runner persistence), so the two never disagree.
//
//   - providerCost non-nil  -> that exact USD (OpenRouter actual cost, preferred)
//   - else model in prices  -> computed from tokens at the table rate
//   - else                  -> 0, ok=false  (unknown model: caller stores/renders an
//     honest absence, never a fabricated "$0")
func CostUSDValue(prices map[string]Price, model string, promptTokens, completionTokens int, providerCost *float64) (usd float64, ok bool) {
	if providerCost != nil {
		return *providerCost, true
	}
	p, found := prices[model]
	if !found {
		return 0, false
	}
	return (float64(promptTokens)*p.InputPer1M + float64(completionTokens)*p.OutputPer1M) / 1_000_000, true
}

// CostUSD reports the turn cost as a display string and an ok flag (D-18/D-23):
//
//   - providerCost non-nil  -> that exact USD (OpenRouter actual cost, preferred)
//   - else model in prices  -> computed from tokens at the table rate
//   - else                  -> "n/a", ok=false  (NEVER "$0" — do not lie)
//
// ok is true only when a real number was produced (provider or table); it is
// false for the unknown-model case so the caller can render the honest "n/a".
func CostUSD(prices map[string]Price, model string, promptTokens, completionTokens int, providerCost *float64) (display string, ok bool) {
	usd, ok := CostUSDValue(prices, model, promptTokens, completionTokens, providerCost)
	if !ok {
		return "n/a", false
	}
	return fmt.Sprintf("$%.6f", usd), true
}
