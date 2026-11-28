package anthropic

import (
	"errors"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
	"github.com/chetto1983/aura/internal/obs"
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
	attrs := append(requestAttrs(built, message),
		"input_tokens", used.input, "cache_read_tokens", used.read, "cache_write_tokens", used.write, "output_tokens", used.output)
	if cost != nil {
		attrs = append(attrs, "cost_usd", *cost)
	} else {
		attrs = append(attrs, "cost_usd", "unknown")
	}
	c.logger().Info("anthropic request", attrs...)
}

// logFailure is the line a request that ends without a response leaves in place of
// logRequest's. message is what arrived before the failure, nil when the request was
// refused before its stream opened. err is never logged: its text carries the provider's
// body. Its status and request id are, and they are already scrubbed of the key.
func (c *Client) logFailure(built builtRequest, message *anthropic.BetaMessage, err error) {
	status, requestID := 0, ""
	if httpErr, ok := errors.AsType[*openai_compat.HTTPError](err); ok {
		status, requestID = httpErr.StatusCode, httpErr.RequestID
	}
	attrs := append(requestAttrs(built, message), "status", status, "error_class", obs.ErrorClass(err), "request_id", requestID)
	c.logger().Warn("anthropic request failed", attrs...)
}

func requestAttrs(built builtRequest, message *anthropic.BetaMessage) []any {
	dropped := 0
	if message != nil {
		for _, transformation := range message.InputTransformations {
			if transformation.Type == "thinking_dropped" {
				dropped++
			}
		}
	}
	return []any{
		"model", built.params.Model, "effort", built.effort,
		"replayed_state_messages", built.replayed, "dropped_blocks", dropped,
	}
}
