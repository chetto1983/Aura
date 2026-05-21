package llm

import (
	"encoding/json"
	"log/slog"
	"strings"
)

// contentBlockJSON is a single content block in the Anthropic / OpenRouter
// multi-part message format. Required for injecting cache_control markers.
type contentBlockJSON struct {
	Type         string            `json:"type"`
	Text         string            `json:"text,omitempty"`
	CacheControl *cacheControlJSON `json:"cache_control,omitempty"`
}

// cacheControlJSON marks a content block for provider-side KV caching.
// Type "ephemeral" is the only value providers currently accept.
type cacheControlJSON struct {
	Type string `json:"type"`
}

// promptTokensDetailsJSON carries per-category prompt token counts.
// Providers that support prompt caching populate CachedTokens to report
// how many input tokens were served from the provider's KV cache instead
// of being re-processed.
type promptTokensDetailsJSON struct {
	CachedTokens int `json:"cached_tokens,omitempty"`
}

// chatMessageCacheJSON is the JSON shape for a message whose content is
// expressed as a content-block array (required for cache_control markers).
type chatMessageCacheJSON struct {
	Role       string             `json:"role"`
	Content    []contentBlockJSON `json:"content"`
	ToolCallID string             `json:"tool_call_id,omitempty"`
}

// MarshalJSON emits the content-block array format when CacheBreakpoint is
// set (so the provider can mark the static prompt prefix for caching), and
// falls back to the standard string-content format otherwise.
func (m chatMessage) MarshalJSON() ([]byte, error) {
	if !m.CacheBreakpoint || m.Content == nil {
		type plain struct {
			Role       string         `json:"role"`
			Content    *string        `json:"content,omitempty"`
			ToolCalls  []toolCallJSON `json:"tool_calls,omitempty"`
			ToolCallID string         `json:"tool_call_id,omitempty"`
		}
		return json.Marshal(plain{
			Role:       m.Role,
			Content:    m.Content,
			ToolCalls:  m.ToolCalls,
			ToolCallID: m.ToolCallID,
		})
	}
	return json.Marshal(chatMessageCacheJSON{
		Role: m.Role,
		Content: []contentBlockJSON{{
			Type:         "text",
			Text:         *m.Content,
			CacheControl: &cacheControlJSON{Type: "ephemeral"},
		}},
		ToolCallID: m.ToolCallID,
	})
}

// DetectCacheSupport returns true when the provider identified by baseURL and
// model is known to accept Anthropic-style cache_control markers on messages.
// This is a heuristic: OpenRouter and Anthropic endpoints are both confirmed;
// DeepSeek and Claude model names trigger it on any endpoint. Unknown
// providers return false so caching is silently skipped (safe default).
func DetectCacheSupport(baseURL, model string) bool {
	if baseURL == "" || model == "" {
		return false
	}
	u := strings.ToLower(strings.TrimSpace(baseURL))
	m := strings.ToLower(strings.TrimSpace(model))
	if strings.Contains(u, "openrouter.ai") {
		return true
	}
	if strings.Contains(u, "anthropic.com") {
		return true
	}
	if strings.Contains(m, "claude") || strings.Contains(m, "deepseek") {
		return true
	}
	return false
}

// injectCacheControl places a cache_control breakpoint on the last system
// message in msgs. The breakpoint tells the provider to cache all content up
// to and including that message (the static prompt block: AGENT+SOUL+TOOLS+
// USER+wiki TOC). Returns a shallow copy of the slice — caller's slice is
// unchanged.
func injectCacheControl(msgs []chatMessage) []chatMessage {
	out := make([]chatMessage, len(msgs))
	copy(out, msgs)
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].Role == "system" && out[i].Content != nil {
			out[i].CacheBreakpoint = true
			return out
		}
	}
	return out
}

// logCacheSkip logs a one-time warning when cache is enabled but the provider
// heuristic says caching is unsupported, so operators know the feature is
// inactive without polling metrics.
func logCacheSkip(baseURL, model string) {
	slog.Default().Warn("prompt cache disabled: provider not recognised as cache-capable",
		"base_url", baseURL, "model", model,
		"hint", "set AURA_PROMPT_CACHE_ENABLED=false to suppress this warning")
}
