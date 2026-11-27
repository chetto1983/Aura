package anthropic

import (
	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/chetto1983/aura/internal/llm"
)

// tokens is one call's usage as Claude bills it: input, cache read and cache write are
// disjoint parts of the prompt.
type tokens struct{ input, read, write, output int }

func (t tokens) prompt() int { return t.input + t.read + t.write }

// callCost prices one call from the catalogue, at the long-prompt rates when this call's
// prompt reaches them. Nil means no honest number exists: the model has no price, or
// the call wrote to the cache and the catalogue publishes no write rate (plan decision
// 6). Cache reads with no published read rate bill at the input rate, as llm.CostUSDValue
// does.
func callCost(prices map[string]llm.Price, model string, used tokens) *float64 {
	price, ok := prices[model]
	if !ok {
		return nil
	}
	rate := price.For(used.prompt())
	if used.write > 0 && rate.CacheWritePer1M == 0 {
		return nil
	}
	readRate := rate.CacheReadPer1M
	if readRate == 0 {
		readRate = rate.InputPer1M
	}
	usd := (float64(used.input)*rate.InputPer1M + float64(used.read)*readRate +
		float64(used.write)*rate.CacheWritePer1M + float64(used.output)*rate.OutputPer1M) / 1_000_000
	return &usd
}

// logRequest writes the one line each Claude request leaves: never content, never the key.
func (c *Client) logRequest(built builtRequest, message *anthropic.BetaMessage, used tokens, cost *float64) {
	dropped := 0
	for _, transformation := range message.InputTransformations {
		if transformation.Type == "thinking_dropped" {
			dropped++
		}
	}
	attrs := []any{
		"model", built.params.Model, "effort", built.effort,
		"replayed_state_messages", built.replayed, "dropped_blocks", dropped,
		"input_tokens", used.input, "cache_read_tokens", used.read, "cache_write_tokens", used.write, "output_tokens", used.output,
	}
	if cost != nil {
		attrs = append(attrs, "cost_usd", *cost)
	} else {
		attrs = append(attrs, "cost_usd", "unknown")
	}
	c.logger.Info("anthropic request", attrs...)
}
