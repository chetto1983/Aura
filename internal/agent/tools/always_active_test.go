package tools

import (
	"sort"
	"strings"
	"testing"
)

// The always-active set is a budget, and it belongs in ONE place. It used to be seventeen
// tools asserted across seven files, each defending its own seat, and nobody was looking at
// the total: 7.821 tokens of schema in every request, ~65% of the fixed prefix, paid to
// answer "ok". skill alone was 1.638.
//
// These four earn it structurally, not by importance:
//   - text_response — without it the model cannot end a turn
//   - tool_search   — the only door to everything deferred; defer it and nothing is reachable
//   - ask_user      — the pause seam for approvals and clarification
//   - read_tool_output — L1 leaves pointers that name this tool by hand; if it needed a
//     tool_search first, the eviction pointer would instruct the model to call something it
//     cannot call
//
// Everything else is discoverable. Adding a fifth is a real decision with a real price, so
// it should require editing this list and reading this comment.
func TestOnlyFourToolsAreAlwaysActive(t *testing.T) {
	t.Parallel()
	want := []string{"ask_user", "read_tool_output", "text_response", "tool_search"}

	// Every tool the daemon can register, constructed the cheap way (no wiring): Spec() is
	// a pure function of the value, and Deferred never depends on collaborators.
	all := []Tool{
		&AskUser{}, &CurrentTime{}, &DocumentSearch{}, &FSEdit{}, &FSGlob{}, &FSGrep{},
		&FSRead{}, &FSWrite{}, &ReadToolOutput{}, &SendFile{}, &ShellExec{}, &SkillTool{},
		&TaskTool{}, &TextResponse{}, &TodoTool{}, &ToolSearch{}, &WebFetch{}, &WebSearch{},
	}

	var got []string
	for _, tool := range all {
		if !tool.Spec().Deferred {
			got = append(got, tool.Spec().Name)
		}
	}
	sort.Strings(got)

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("always-active set = %v, want %v\n"+
			"a new entry here is paid on EVERY turn, cold and cached; if it is genuinely "+
			"structural, extend want and say why in the comment above", got, want)
	}
}
