package openai_compat

import "github.com/chetto1983/aura/internal/llm"

// usageWire mirrors the OpenRouter usage object delivered in the LAST SSE message
// (RESEARCH Pitfall 4). Only consumed fields are declared; unknown fields
// (audio_tokens, reasoning_tokens, cost_details, total_tokens) are ignored by
// encoding/json. cached_tokens (cache READS) is intentionally distinct from
// cache_write_tokens (cache WRITES, billed on the first turn that establishes an
// entry) — conflating them misreports the cache-hit ratio.
type usageWire struct {
	PromptTokens        int      `json:"prompt_tokens"`
	CompletionTokens    int      `json:"completion_tokens"`
	TotalTokens         int      `json:"total_tokens"`
	Cost                *float64 `json:"cost"`
	PromptTokensDetails struct {
		CachedTokens     int `json:"cached_tokens"`
		CacheWriteTokens int `json:"cache_write_tokens"`
	} `json:"prompt_tokens_details"`
}

// Usage is the agent/REPL-facing token+cost summary captured from the final
// usage chunk. Cost is the provider's reported figure (preferred over recompute,
// D-18); it is nil when the provider sent no cost (the caller falls back to the
// price table, never reporting $0 for an unknown model). CachedTokens is the
// cache-READ count (prompt_tokens_details.cached_tokens), NOT cache writes.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	CachedTokens     int
	Cost             *float64
}

// toUsage projects the wire object onto the exported summary, mapping
// cached_tokens (reads) onto CachedTokens and discarding cache_write_tokens
// (Phase 6 KV builder owns cache-write observability — RESEARCH OQ#2).
func (w *usageWire) toUsage() Usage {
	return Usage{
		PromptTokens:     w.PromptTokens,
		CompletionTokens: w.CompletionTokens,
		CachedTokens:     w.PromptTokensDetails.CachedTokens,
		Cost:             w.Cost,
	}
}

// toLLMUsage projects the captured Usage onto the provider-neutral llm.Usage the
// agent reads off the trailing stream chunk (Req#12). Same fields, no provider
// coupling — the agent never imports openai_compat.
func (u Usage) toLLMUsage() llm.Usage {
	return llm.Usage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		CachedTokens:     u.CachedTokens,
		Cost:             u.Cost,
	}
}
