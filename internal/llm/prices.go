package llm

import "fmt"

// CostStatus records why a model profile does or does not have a numeric rate.
// Numeric zero alone cannot distinguish included local compute from an unknown
// remote price, while subscription-backed models may have no per-token bill at all.
type CostStatus string

// Cost status values distinguish measured rates from included or unknown pricing.
const (
	CostStatusUnknown              CostStatus = "unknown"
	CostStatusRateEstimate         CostStatus = "rate-estimate"
	CostStatusLocalIncluded        CostStatus = "local-included"
	CostStatusSubscriptionIncluded CostStatus = "subscription-included"
)

// Price is a per-model fallback rate in USD per 1,000,000 tokens (D-23). It is the
// safety net only — OpenRouter's reported usage.cost is always preferred when present
// (D-18), because it already nets cache reads/writes and inference.
//
// There is no hardcoded seed: rates are read at runtime from GET /models for the
// configured model (amendment #93). A model with no resolved Price renders the honest
// "n/a", never a stale guess and never $0.
type Price struct {
	InputPer1M  float64 `json:"input_per_1m"`
	OutputPer1M float64 `json:"output_per_1m"`
	// CacheReadPer1M is the discounted rate for prompt tokens served from cache
	// (`pricing.input_cache_read`). Zero means the model publishes no cache rate — 155
	// of the 367 listed models omit it — and cache reads then bill at InputPer1M.
	//
	// Omitting this is not a rounding error. Measured over 30 days on the live account:
	// 54.86M prompt tokens of which 40.55M were cache reads, 0.64M completion, real cost
	// $2.889. Billing every prompt token at the full rate yields $7.86 — a 2.7x
	// over-report. With the cache rate the same arithmetic yields $3.32.
	CacheReadPer1M float64 `json:"cache_read_per_1m"`
	// CacheWritePer1M is the rate for prompt tokens written to the cache
	// (`pricing.input_cache_write`, the 5-minute tier). Zero means none is published.
	CacheWritePer1M float64 `json:"cache_write_per_1m,omitempty"`
	// LongPrompt replaces every rate above once a prompt reaches MinPromptTokens.
	LongPrompt *LongPromptPrice `json:"long_prompt,omitempty"`
}

// LongPromptPrice is the rate card a model switches to for prompts of at least
// MinPromptTokens (`pricing.overrides[]`).
type LongPromptPrice struct {
	MinPromptTokens                                          int
	InputPer1M, OutputPer1M, CacheReadPer1M, CacheWritePer1M float64
}

// For is the rate card that bills one call whose prompt is promptTokens long: the long
// prompt rates once the prompt reaches their threshold, p itself below it.
func (p Price) For(promptTokens int) Price {
	long := p.LongPrompt
	if long == nil || promptTokens < long.MinPromptTokens {
		return p
	}
	return Price{InputPer1M: long.InputPer1M, OutputPer1M: long.OutputPer1M, CacheReadPer1M: long.CacheReadPer1M, CacheWritePer1M: long.CacheWritePer1M}
}

// costUSDValue reports the numeric turn cost in USD and an ok flag, with the D-18/D-23
// precedence below. It is TurnCostUSD's core on every route, so the displayed footer
// (CostUSD) and the persisted cost (runner persistence) never disagree.
//
//   - usage.Cost non-nil    -> that exact USD (OpenRouter actual cost, preferred)
//   - else model in prices  -> computed from tokens at the resolved rate
//   - else                  -> 0, ok=false  (caller stores/renders an honest absence,
//     never a fabricated "$0")
//
// It takes the whole Usage rather than loose token counts so that adding a token class
// cannot silently transpose two int arguments at a call site.
func costUSDValue(prices map[string]Price, model string, usage Usage) (usd float64, ok bool) {
	if usage.Cost != nil {
		return *usage.Cost, true
	}
	p, found := prices[model]
	if !found {
		return 0, false
	}

	// OpenRouter's prompt_tokens is the TOTAL input; cached_tokens says how many of
	// those were served from cache. They are not additive.
	cached := max(0, min(usage.CachedTokens, usage.PromptTokens))
	cachedRate := p.CacheReadPer1M
	if cachedRate == 0 {
		cachedRate = p.InputPer1M
	}
	fresh := usage.PromptTokens - cached

	total := float64(fresh)*p.InputPer1M +
		float64(cached)*cachedRate +
		float64(usage.CompletionTokens)*p.OutputPer1M
	return total / 1_000_000, true
}

// TurnCostUSD is a turn's cost on provider's route, from the usage its calls summed: the
// rule every reader of a turn's cost shares (persisted turn, REPL footer, Telegram /cost).
// It is costUSDValue's precedence, except that on the anthropic route a turn none of whose
// calls carried a cost is unknown, never priced from the table: that client prices every
// call itself, cache writes included, and leaves a call unpriced only when the catalogue has
// no honest number for it (spec 2026-11-27-anthropic-provider §7.4, plan decision 6).
func TurnCostUSD(provider string, prices map[string]Price, model string, usage Usage) (usd float64, ok bool) {
	if usage.Cost == nil && provider == AnthropicProvider {
		return 0, false
	}
	return costUSDValue(prices, model, usage)
}

// CostUSD reports the turn cost as a display string and an ok flag (D-18/D-23), by
// TurnCostUSD:
//
//   - usage.Cost non-nil    -> that exact USD (OpenRouter actual cost, preferred)
//   - else model in prices  -> computed from tokens at the resolved rate, except on the
//     anthropic route
//   - else                  -> "n/a", ok=false  (NEVER "$0" — do not lie)
//
// ok is true only when a real number was produced (provider or table); it is false for
// the unknown case so the caller can render the honest "n/a".
func CostUSD(provider string, prices map[string]Price, model string, usage Usage) (display string, ok bool) {
	usd, ok := TurnCostUSD(provider, prices, model, usage)
	if !ok {
		return "n/a", false
	}
	return fmt.Sprintf("$%.6f", usd), true
}
