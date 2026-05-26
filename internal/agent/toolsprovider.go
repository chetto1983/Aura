package agent

import (
	"log/slog"

	"github.com/aura/aura/internal/llm"
)

// AlwaysOnCore is the seed of the per-turn tool pool.
//
// Keep this deliberately small. The full registry is still reachable through
// tool_search plus the loop's permissive resolver, but every item here is sent
// as a JSON schema on every LLM call. Treat it as the hot path, not the catalog.
var AlwaysOnCore = []string{
	"text_response",
	"search",
	"web",
	"source",
	"ask_user",
	"tool_search",
}

// MakeToolsProvider returns the per-turn ToolsProvider closure consumed
// by agent.Options.ToolsProvider. Post-rollout the closure is
// stateless and trivial: it always returns the always-on set. Pool
// growth happens inside agent.Run (see toolPool.AbsorbToolSearchResult
// and EnsureLoaded).
//
// The signature still takes function-typed dependencies so existing
// tests (toolsprovider_test.go) keep working with stubs; searchFn /
// latestUserMsgFn / topKFn are accepted but ignored to preserve the
// call shape while we delete the retrieval branch.
func MakeToolsProvider(
	coreNames []string,
	_ any, // searchFn — unused after deferred-tools rollout, kept for caller signature stability
	defsForFn func(names []string) []llm.ToolDefinition,
	_ func() []llm.ToolDefinition, // defsAllFn — fallback retired, pool grows via permissive load
	_ func() string, // latestUserMsgFn — retrieval no longer reads the message
	_ func() int, // topKFn — unused, kept for caller signature stability
	_ *slog.Logger,
) func() []llm.ToolDefinition {
	return func() []llm.ToolDefinition {
		return defsForFn(coreNames)
	}
}
