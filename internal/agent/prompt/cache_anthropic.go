package prompt

import "github.com/chetto1983/aura/internal/llm"

// cacheControlEphemeral is the Anthropic-direct cache_control marker for the
// tools + system breakpoint.
const cacheControlEphemeral = "ephemeral"

// injectCacheControl is the provider-aware cache seam (D-03). Under any provider
// other than anthropic it leaves req untouched (the OpenRouter SC#3 assertion: no
// cache_control on the wire). For anthropic it marks the tools-side breakpoint and
// records where the run's byte-stable prefix (stable) and the messages before the current
// user message (loaded: the system prompt, the stored turns and the transient memory block)
// end; the wire client turns those positions into message breakpoints. It never edits
// a message: a marker inside history would move every round (the poisoning the 06-05
// gate prevents).
func injectCacheControl(req *llm.Request, provider string, stable, loaded int) {
	if provider != llm.AnthropicProvider {
		return
	}
	req.ToolsCacheControl = cacheControlEphemeral
	req.StablePrefixMessages = stable
	req.LoadedHistoryMessages = loaded
}
