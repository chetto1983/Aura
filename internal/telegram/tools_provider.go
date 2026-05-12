package telegram

import (
	"log/slog"
	"strings"

	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/tools"
)

// alwaysOnCore lists the 6 always-injected tool names. Phase 2 D-22 lock
// (revised 2026-05-11 plan-checker iteration 3 — see CONTEXT.md D-22 note).
// Every name MUST exist in the live tool registry. Verified at planning
// time via `grep -rho 'return "[a-z_]+"' internal/tools/*.go`:
//
//	wiki_page               (Wave 2.6 — replaces legacy write_wiki_page)
//	search_memory           (internal/tools/memory_search.go:105)
//	list_sources            (internal/tools/source.go:357)
//	read_source             (internal/tools/source.go:237)
//	schedule_task           (internal/tools/scheduler.go:108)
//	request_dashboard_token (internal/tools/auth.go:43)
//
// Order is preserved as documented; downstream prompt formatting may rely on it.
var alwaysOnCore = []string{
	"wiki_page",
	"search_memory",
	"list_sources",
	"read_source",
	"schedule_task",
	"request_dashboard_token",
}

// makeToolsProvider returns the per-turn ToolsProvider closure consumed by
// agentloop.Options.ToolsProvider. WARNING 4 of 2026-05-11 plan revision 2:
// this helper lives in the sibling file tools_provider.go (NOT conversation.go)
// because conversation.go is 507 LOC and inlining the helper would push it
// close to the 600 LOC god-class ceiling.
//
// WARNING 5 of 2026-05-11 plan revision 2: the helper takes FUNCTION-TYPED
// dependencies rather than a *Bot or *tools.Registry. This avoids extracting
// an interface from the concrete *tools.Registry (bot.go:46) just to make
// the helper testable. In tests, pass call-counting stub functions; in
// production, pass b.tools.Search / b.tools.DefinitionsFor / b.tools.Definitions
// and convCtx.LatestUserMessageText as method values.
//
// The closure is invoked ONCE per turn:
//
//   - Cold start (no user message yet) → core only (Pitfall #6).
//   - Retrieval unavailable / empty (Qdrant down OR no semantic match) →
//     FULL toolset (D-25). Uniform len()==0 check covers BOTH nil and empty
//     cases (WARNING 11 of 2026-05-10 plan revision).
//   - Normal turn → core ∪ top-K retrieved (K read live from topKFn so the
//     dashboard can re-tune retrieval breadth without a restart).
//     retrievedNames are collected and passed to defsForFn in a SINGLE
//     batched call (WARNING 16 of 2026-05-10 plan revision).
//
// The closure passes coreNames... as searchFn's excluded variadic so the
// retrieval layer does NOT double-inject any core tool.
//
// searchFn must mirror Registry.Search (verified at registry_search.go:19):
//
//	func(query string, limit int, excluded ...string) []tools.ToolSearchResult
//
// NO ctx parameter — BLOCKER 2 of 2026-05-11 revision 2 verified the signature.
func makeToolsProvider(
	coreNames []string,
	searchFn func(query string, limit int, excluded ...string) []tools.ToolSearchResult,
	defsForFn func(names []string) []llm.ToolDefinition,
	defsAllFn func() []llm.ToolDefinition,
	latestUserMsgFn func() string,
	topKFn func() int,
	logger *slog.Logger,
) func() []llm.ToolDefinition {
	return func() []llm.ToolDefinition {
		coreDefs := defsForFn(coreNames)
		latestUserMsg := latestUserMsgFn()
		if strings.TrimSpace(latestUserMsg) == "" {
			// D-23 / Pitfall #6: cold-start, inject core only.
			return coreDefs
		}
		topK := 5
		if topKFn != nil {
			if v := topKFn(); v > 0 {
				topK = v
			}
		}
		retrieved := searchFn(latestUserMsg, topK, coreNames...)
		if len(retrieved) == 0 {
			// D-25 / WARNING 11: Qdrant down OR no semantic match.
			// Both routes collapse to FULL toolset; never fail the turn.
			if logger != nil {
				logger.Warn("tools_provider_fallback", "reason", "retrieval_unavailable_or_empty")
			}
			return defsAllFn()
		}
		// WARNING 16: collect names, batch defsForFn in ONE call.
		retrievedNames := make([]string, 0, len(retrieved))
		for _, r := range retrieved {
			retrievedNames = append(retrievedNames, r.Name)
		}
		retrievedDefs := defsForFn(retrievedNames)
		out := make([]llm.ToolDefinition, 0, len(coreDefs)+len(retrievedDefs))
		out = append(out, coreDefs...)
		out = append(out, retrievedDefs...)
		return out
	}
}
