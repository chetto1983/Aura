package agent

import (
	"log/slog"

	"github.com/aura/aura/internal/llm"
)

// AlwaysOnCore is the seed of the per-turn tool pool.
//
// Deferred-tools rollout (2026-05-12): the seed is now JUST tool_search.
// Every other tool is discovered through one of two paths:
//
//  1. The model reads the catalog manifest in the system prompt and calls
//     a tool by name directly. The agent loop's permissive-load path
//     resolves the name via Registry.DefinitionFor and adds the schema
//     to the pool for this turn.
//
//  2. The model calls tool_search(query) to retrieve candidate schemas;
//     the executor's result is absorbed by the pool so the next LLM
//     iteration sees the discovered tools in its tools array.
//
// Rationale (from the embed-vs-LLM benchmark, 2026-05-12): mini-LLM
// rerankers on CPU exceed 1500ms p95 and are not viable as the routing
// backend. Embedding cosine via the registry's existing hybrid Search
// hits ~80ms p95 with 90% Hit@5 on a 64-query labeled dataset; the
// model's own judgment over the manifest + permissive load covers the
// remainder. No retrieval logic remains in toolsprovider — the agent
// loop owns pool growth.
var AlwaysOnCore = []string{
	"tool_search",
	"search_memory",
	"wiki_subgraph",
	"source",
	"wiki_page",
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
	_ func() int, // topKFn — top-K is a property of tool_search, not the seed
	_ *slog.Logger,
) func() []llm.ToolDefinition {
	return func() []llm.ToolDefinition {
		return defsForFn(coreNames)
	}
}
