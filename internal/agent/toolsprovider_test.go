package agent

import (
	"io"
	"log/slog"
	"testing"

	"github.com/aura/aura/internal/llm"
)

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// coreStubDefs returns one llm.ToolDefinition per name in the input slice.
// Used as defsForFn stub.
func coreStubDefs(names []string) []llm.ToolDefinition {
	out := make([]llm.ToolDefinition, len(names))
	for i, n := range names {
		out[i] = llm.ToolDefinition{Name: n, Description: "stub"}
	}
	return out
}

func TestAlwaysOnCore_ContainsWikiFastPath(t *testing.T) {
	// The seed must include the wiki/source retrieval path so ordinary wiki Q&A
	// can resolve in <=2 tool calls without extra discovery overhead.
	want := []string{"search", "source", "wiki_page"}
	seen := make(map[string]bool, len(AlwaysOnCore))
	for _, name := range AlwaysOnCore {
		seen[name] = true
	}
	for _, name := range want {
		if !seen[name] {
			t.Fatalf("AlwaysOnCore missing %q; got %v", name, AlwaysOnCore)
		}
	}
}

func TestToolsProvider_ReturnsAlwaysOnSeed(t *testing.T) {
	// The closure returns exactly the always-on set, stateless and
	// message-independent. Pool growth happens in agent.Run via permissive load.
	provider := MakeToolsProvider(
		AlwaysOnCore,
		nil,          // searchFn ignored
		coreStubDefs, // defsForFn
		nil,          // defsAllFn retired
		nil,          // latestUserMsgFn — message no longer drives the seed
		nil,          // topKFn — unused
		silentLogger(),
	)
	defs := provider()
	if len(defs) != len(AlwaysOnCore) {
		t.Fatalf("provider returned %d defs, want %d (AlwaysOnCore)", len(defs), len(AlwaysOnCore))
	}
	wantSet := make(map[string]bool, len(AlwaysOnCore))
	for _, n := range AlwaysOnCore {
		wantSet[n] = true
	}
	for _, d := range defs {
		if !wantSet[d.Name] {
			t.Fatalf("unexpected tool %q in always-on seed: %+v", d.Name, defs)
		}
	}
}

func TestToolsProvider_DoesNotInvokeIgnoredCallbacks(t *testing.T) {
	// Regression: prior implementation called latestUserMsgFn and
	// searchFn on every turn. After the rollout, those should never
	// be invoked from the closure.
	provider := MakeToolsProvider(
		AlwaysOnCore,
		nil,
		coreStubDefs,
		func() []llm.ToolDefinition {
			t.Fatal("defsAllFn must not be called — fallback retired")
			return nil
		},
		func() string {
			t.Fatal("latestUserMsgFn must not be called — message no longer drives the seed")
			return ""
		},
		func() int {
			t.Fatal("topKFn must not be called")
			return 0
		},
		silentLogger(),
	)
	if defs := provider(); len(defs) == 0 {
		t.Fatal("provider returned no defs")
	}
}
