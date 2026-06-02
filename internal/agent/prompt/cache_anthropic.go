package prompt

import "github.com/chetto1983/aura/internal/llm"

// providerAnthropic is the only provider for which the cache_control seam fires.
// Day-1 default is openrouter (internal/llm/config.go), under which this branch
// is a pure no-op — never hardcode the provider, always read cfg.Provider.
const providerAnthropic = "anthropic"

// cacheControlEphemeral is the Anthropic-direct cache_control marker. On
// activation (Slice 13) it tags the system block + the LAST tool definition as a
// single cache breakpoint; it is NEVER attached to per-turn history messages,
// which would shift the breakpoint every turn (the exact poisoning the 06-05
// gate prevents).
const cacheControlEphemeral = "ephemeral"

// injectCacheControl is the dormant provider-aware seam (D-03). Under any
// provider other than anthropic it leaves req untouched (the OpenRouter SC#3
// assertion: no cache_control on the wire). For anthropic it sets only the
// tools-side ToolsCacheControl marker; the Anthropic-native block-array wire
// translation (system block + last tool def) is Slice 13's job — this phase only
// proves the branch exists and never touches history (D-03/D-03a).
func injectCacheControl(req *llm.Request, provider string) {
	if provider != providerAnthropic {
		return
	}
	req.ToolsCacheControl = cacheControlEphemeral
}
