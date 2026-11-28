package llm

// pricingWire is the OpenRouter `pricing` object. Rates are JSON strings per token.
// `overrides` carries long-prompt rate cards (Haiku 5.5 above 100,000 prompt tokens).
type pricingWire struct {
	Prompt          string `json:"prompt"`
	Completion      string `json:"completion"`
	InputCacheRead  string `json:"input_cache_read"`
	InputCacheWrite string `json:"input_cache_write"`
	Overrides       []struct {
		MinPromptTokens int    `json:"min_prompt_tokens"`
		Prompt          string `json:"prompt"`
		Completion      string `json:"completion"`
		InputCacheRead  string `json:"input_cache_read"`
		InputCacheWrite string `json:"input_cache_write"`
	} `json:"overrides"`
}

// price builds the per-1M Price once input and output rates parsed. An absent cache
// rate stays zero, as it always has.
//
// The API reference defines no order for `overrides`. Measured on GET /models
// 2026-11-28 (476 models, 88 with overrides): most publish one prompt-size tier, two
// qwen models publish two (32k, then 128k or 256k), and four publish UTC time windows with no
// min_prompt_tokens. A time window is not a prompt-size tier and is skipped. The kept
// tier is the lowest threshold, where the base rate stops applying, which is what
// anthropicWindow sizes the Claude window against. A higher tier is not modelled:
// a prompt above it bills at the lower tier's rates. No Claude model publishes one.
func (p pricingWire) price(in, out float64) Price {
	read, _ := ratePer1M(p.InputCacheRead)
	write, _ := ratePer1M(p.InputCacheWrite)
	price := Price{InputPer1M: in, OutputPer1M: out, CacheReadPer1M: read, CacheWritePer1M: write}
	for _, o := range p.Overrides {
		oin, inErr := ratePer1M(o.Prompt)
		oout, outErr := ratePer1M(o.Completion)
		if o.MinPromptTokens <= 0 || inErr != nil || outErr != nil {
			continue
		}
		if price.LongPrompt != nil && price.LongPrompt.MinPromptTokens <= o.MinPromptTokens {
			continue
		}
		oread, _ := ratePer1M(o.InputCacheRead)
		owrite, _ := ratePer1M(o.InputCacheWrite)
		price.LongPrompt = &LongPromptPrice{
			MinPromptTokens: o.MinPromptTokens, InputPer1M: oin, OutputPer1M: oout,
			CacheReadPer1M: oread, CacheWritePer1M: owrite,
		}
	}
	return price
}
