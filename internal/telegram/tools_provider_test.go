package telegram

import (
	"io"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/tools"
)

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func fixedTopK(k int) func() int {
	return func() int { return k }
}

// coreStubDefs returns one llm.ToolDefinition per name in the input slice.
// Used as defsForFn stub. Asymmetric to defsAllFn (which returns ALL tools
// including non-core) so tests can distinguish the FULL-fallback path.
func coreStubDefs(names []string) []llm.ToolDefinition {
	out := make([]llm.ToolDefinition, len(names))
	for i, n := range names {
		out[i] = llm.ToolDefinition{Name: n, Description: "stub"}
	}
	return out
}

func TestToolsProvider_ColdStart(t *testing.T) {
	// No user message → cold start → core (6) only.
	searchFn := func(q string, limit int, excluded ...string) []tools.ToolSearchResult {
		t.Fatal("searchFn must not be called on cold-start path")
		return nil
	}
	defsAllFn := func() []llm.ToolDefinition {
		t.Fatal("defsAllFn must not be called on cold-start path")
		return nil
	}
	latestUserMsgFn := func() string { return "" }

	provider := makeToolsProvider(alwaysOnCore, searchFn, coreStubDefs, defsAllFn, latestUserMsgFn, fixedTopK(5), silentLogger())
	defs := provider()
	if len(defs) != 6 {
		t.Fatalf("cold-start returned %d defs, want 6", len(defs))
	}
	wantSet := map[string]bool{
		"write_wiki_page": true, "search_memory": true, "list_sources": true,
		"read_source": true, "schedule_task": true,
		"request_dashboard_token": true,
	}
	for _, d := range defs {
		if !wantSet[d.Name] {
			t.Fatalf("unexpected tool %q in cold-start core: %+v", d.Name, defs)
		}
	}
}

func TestToolsProvider_QdrantDown_FullToolset(t *testing.T) {
	// searchFn returns empty → fallback to FULL toolset.
	// WARNING 11 of 2026-05-10 plan revision: nil and empty both route here.
	searchFn := func(q string, limit int, excluded ...string) []tools.ToolSearchResult {
		return nil // simulate Qdrant down — Registry.Search returns nil
	}
	// defsAllFn returns 15 tools (more than the 6 core) so we can distinguish
	// the FULL-fallback path from the core-only path.
	defsAllFn := func() []llm.ToolDefinition {
		out := make([]llm.ToolDefinition, 15)
		for i := range out {
			out[i] = llm.ToolDefinition{Name: "tool_full_" + string(rune('a'+i))}
		}
		return out
	}
	latestUserMsgFn := func() string { return "send an email" }

	provider := makeToolsProvider(alwaysOnCore, searchFn, coreStubDefs, defsAllFn, latestUserMsgFn, fixedTopK(5), silentLogger())
	defs := provider()
	if len(defs) != 15 {
		t.Fatalf("FULL-fallback returned %d defs, want 15 (defsAllFn output)", len(defs))
	}
}

func TestToolsProvider_NormalRetrieval_BatchedDefinitionsFor(t *testing.T) {
	// searchFn returns one retrieved result. defsForFn counts how many
	// times it is invoked with non-core names. WARNING 16 of 2026-05-10
	// plan revision: ONE batched call on the retrieved path.
	searchFn := func(q string, limit int, excluded ...string) []tools.ToolSearchResult {
		return []tools.ToolSearchResult{{Name: "web_search", Description: "web"}}
	}
	var retrievedSideCalls atomic.Int32
	defsForFn := func(names []string) []llm.ToolDefinition {
		// Count only calls whose first name is NOT in the core set.
		// A simpler counter: count calls where the slice is exactly
		// ["web_search"]. The core call has 6 entries; the retrieved
		// call has 1.
		if len(names) == 1 && names[0] == "web_search" {
			retrievedSideCalls.Add(1)
		}
		return coreStubDefs(names)
	}
	defsAllFn := func() []llm.ToolDefinition {
		t.Fatal("defsAllFn must not be called on normal path")
		return nil
	}
	latestUserMsgFn := func() string { return "search the web for AI safety news" }

	provider := makeToolsProvider(alwaysOnCore, searchFn, defsForFn, defsAllFn, latestUserMsgFn, fixedTopK(5), silentLogger())
	defs := provider()
	if len(defs) != 7 {
		t.Fatalf("normal returned %d defs, want 7 (core 6 + retrieved 1)", len(defs))
	}
	// WARNING 16 invariant: exactly ONE batched defsForFn call on the retrieved path.
	if got := retrievedSideCalls.Load(); got != 1 {
		t.Fatalf("retrieved-side defsForFn calls = %d, want exactly 1 (WARNING 16)", got)
	}
}

func TestToolsProvider_SearchExcludesCoreNames(t *testing.T) {
	// searchFn must receive the core names as the excluded variadic, so
	// retrieval does not double-inject anything in alwaysOnCore.
	var capturedExcluded []string
	searchFn := func(q string, limit int, excluded ...string) []tools.ToolSearchResult {
		capturedExcluded = append([]string(nil), excluded...)
		return []tools.ToolSearchResult{{Name: "web_search"}}
	}
	defsAllFn := func() []llm.ToolDefinition { return nil }
	latestUserMsgFn := func() string { return "any non-empty user message" }

	provider := makeToolsProvider(alwaysOnCore, searchFn, coreStubDefs, defsAllFn, latestUserMsgFn, fixedTopK(5), silentLogger())
	_ = provider()
	if len(capturedExcluded) != len(alwaysOnCore) {
		t.Fatalf("excluded len = %d, want %d (all core names passed as variadic)", len(capturedExcluded), len(alwaysOnCore))
	}
}

func TestToolsProvider_TopKReadDynamically(t *testing.T) {
	// topKFn is read on every invocation so dashboard changes to
	// TOOL_SEARCH_TOP_K propagate without restart. We flip the returned
	// value between two provider calls and assert searchFn sees both.
	var capturedLimits []int
	searchFn := func(q string, limit int, excluded ...string) []tools.ToolSearchResult {
		capturedLimits = append(capturedLimits, limit)
		return []tools.ToolSearchResult{{Name: "web_search"}}
	}
	defsAllFn := func() []llm.ToolDefinition { return nil }
	latestUserMsgFn := func() string { return "any non-empty user message" }

	var topK atomic.Int32
	topK.Store(5)
	topKFn := func() int { return int(topK.Load()) }

	provider := makeToolsProvider(alwaysOnCore, searchFn, coreStubDefs, defsAllFn, latestUserMsgFn, topKFn, silentLogger())
	_ = provider()
	topK.Store(20)
	_ = provider()

	if len(capturedLimits) != 2 {
		t.Fatalf("captured %d limits, want 2", len(capturedLimits))
	}
	if capturedLimits[0] != 5 || capturedLimits[1] != 20 {
		t.Fatalf("limits = %v, want [5 20] (topK read live per turn)", capturedLimits)
	}
}

func TestToolsProvider_TopKZeroFallsBackToFive(t *testing.T) {
	// A misconfigured topKFn (nil result, 0, negative) must not zero out
	// retrieval — the provider keeps a sane default of 5 so a bad dashboard
	// edit can't reduce tool surface to just the core.
	var capturedLimit int
	searchFn := func(q string, limit int, excluded ...string) []tools.ToolSearchResult {
		capturedLimit = limit
		return []tools.ToolSearchResult{{Name: "web_search"}}
	}
	defsAllFn := func() []llm.ToolDefinition { return nil }
	latestUserMsgFn := func() string { return "any non-empty user message" }

	provider := makeToolsProvider(alwaysOnCore, searchFn, coreStubDefs, defsAllFn, latestUserMsgFn, func() int { return 0 }, silentLogger())
	_ = provider()
	if capturedLimit != 5 {
		t.Fatalf("topK=0 produced limit=%d, want fallback 5", capturedLimit)
	}
}
