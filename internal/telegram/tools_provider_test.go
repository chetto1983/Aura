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

	provider := makeToolsProvider(alwaysOnCore, searchFn, coreStubDefs, defsAllFn, latestUserMsgFn, silentLogger())
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

	provider := makeToolsProvider(alwaysOnCore, searchFn, coreStubDefs, defsAllFn, latestUserMsgFn, silentLogger())
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

	provider := makeToolsProvider(alwaysOnCore, searchFn, defsForFn, defsAllFn, latestUserMsgFn, silentLogger())
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

	provider := makeToolsProvider(alwaysOnCore, searchFn, coreStubDefs, defsAllFn, latestUserMsgFn, silentLogger())
	_ = provider()
	if len(capturedExcluded) != len(alwaysOnCore) {
		t.Fatalf("excluded len = %d, want %d (all core names passed as variadic)", len(capturedExcluded), len(alwaysOnCore))
	}
}
