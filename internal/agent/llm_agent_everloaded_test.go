package agent

import (
	"sort"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

func everLoadedNames(t *testing.T, hist []llm.Message, reg *tools.Registry) []string {
	t.Helper()
	set := deriveEverLoaded(hist, reg)
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// The defect these pin, measured on a live conversation (thread 019fae9e, seq 27-45): the
// model looked up web_search and web_fetch, used only web_search, and two turns later
// called web_fetch — reading its schema straight out of its own transcript, where it still
// was. The dispatch gate consulted `activated`, which had dropped web_fetch under the
// use-promotes rule, so the call was bounced with tool_not_loaded. The model then spent a
// whole LLM round trip re-searching a schema it was already looking at.
//
// `activated` is right to forget: it decides what goes in the MANIFEST, which has to stay
// small. The gate is asking a different question — are these arguments grounded? — and for
// that, forgetting buys nothing.
func TestEverLoadedKeepsWhatActivatedForgets(t *testing.T) {
	t.Parallel()
	reg := decayRegistry()

	// Exactly the live shape: look up two, use one, look up a third.
	hist := append(searchTurn("s1", "alpha"), callTurn("alpha"))
	hist = append(hist, searchTurn("s2", "beta")...)
	hist = append(hist, searchTurn("s3", "gamma")...)

	if got := activatedNames(t, hist, reg); len(got) != 2 || got[0] != "alpha" || got[1] != "gamma" {
		t.Fatalf("activated = %v, want [alpha gamma] — beta drops, and that is correct", got)
	}
	if got := everLoadedNames(t, hist, reg); len(got) != 3 || got[0] != "alpha" || got[1] != "beta" || got[2] != "gamma" {
		t.Fatalf("everLoaded = %v, want all three: every schema is still in the transcript", got)
	}
}

// The gate must follow everLoaded, not activated — otherwise the set above is inert.
func TestDispatchGateAcceptsATranscriptGroundedCall(t *testing.T) {
	t.Parallel()
	reg := decayRegistry()
	hist := append(searchTurn("s1", "beta"), searchTurn("s2", "gamma")...)

	a := &LlmAgent{
		registry:   reg,
		activated:  deriveActivated(hist, reg),
		everLoaded: deriveEverLoaded(hist, reg),
	}

	if _, promoted := a.activated["beta"]; promoted {
		t.Fatal("precondition: beta must have dropped out of activated for this test to mean anything")
	}
	if a.isDeferredUnloaded("beta") {
		t.Error("beta bounced: its schema is in the transcript, so its arguments are grounded")
	}
	// The hallucination guard is the whole reason the gate exists, and it must survive.
	if !a.isDeferredUnloaded("alpha") {
		t.Error("alpha accepted: it was never searched, so any arguments for it are invented")
	}
}

// Load-then-call inside ONE turn cannot wait for the next construction to rebuild the set
// from history — promoteFromMeta has to seed both.
func TestPromoteFromMetaSeedsTheGateWithinTheTurn(t *testing.T) {
	t.Parallel()
	a := &LlmAgent{registry: decayRegistry()}
	meta := tools.ToolResultMeta{tools.MetaActivatedTools: []string{"beta"}}
	a.promoteFromMeta(&meta)

	if a.isDeferredUnloaded("beta") {
		t.Error("beta bounced in the very turn that loaded it")
	}
}

// A tool cannot mint its own grant by printing a spec block: everLoaded anchors on the
// tool_call that produced the result, exactly as deriveActivated does. Without this a bare
// `echo "## gamma"` from any tool would hand the model permission to call gamma with
// fabricated arguments — the hallucination the gate exists to stop.
func TestEverLoadedIgnoresASpecBlockFromANonSearchTool(t *testing.T) {
	t.Parallel()
	reg := decayRegistry()
	hist := []llm.Message{
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("x1", "plain", "{}")}},
		{Role: llm.RoleTool, ToolCallID: "x1", Content: searchResult("gamma")},
	}
	if got := everLoadedNames(t, hist, reg); len(got) != 0 {
		t.Fatalf("everLoaded = %v, want empty: the spec block did not come from tool_search", got)
	}
}
