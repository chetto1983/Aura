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
// Four earn it STRUCTURALLY — without them the loop itself does not work:
//   - text_response — without it the model cannot end a turn
//   - tool_search   — the only door to everything deferred; defer it and nothing is reachable
//   - ask_user      — the pause seam for approvals and clarification
//   - read_tool_output — L1 leaves pointers that name this tool by hand; if it needed a
//     tool_search first, the eviction pointer would instruct the model to call something it
//     cannot call
//
// Four more earn it OPERATIONALLY, and the difference matters: the first four let her run,
// these four let her work. Cutting to four alone was measured on 2026-08-03 and it does not
// hold — a manifest that can only ask, page, reply and search means every substantive turn
// opens with a search for how to do anything, and she wandered: asked for a customer code
// that was in the operator's own spreadsheet she went to memory, then to the PUBLIC WEB,
// then listed the whole filesystem, and reached the document library on the fourth attempt.
//
//   - shell_exec      — the most-used tool in the system; the terminal is how work happens
//   - fs_read         — reading a file whose path she already has is not a choice between
//     tools, it is the next step
//   - document_search — the operator's own documents are what this deployment is FOR, and
//     the entry point to them cannot be something she has to think of looking for
//   - document_open   — the second half of one motion: search says WHICH file, open puts it
//     on disk. Split across a search round trip, a single question costs two.
//
// The arithmetic, measured. These four cost 7.594 characters ≈ 1.898 tokens on every turn,
// cold and cached. One avoided tool_search costs a whole model call: the measured turn
// re-sent a 5.810-token prompt and spent ~3s to fetch a schema. Break-even is one avoided
// search every three turns, and the traces show more than one per turn. Anthropic's own
// guidance for this pattern is to keep the three to five most-used tools loaded; Aura had
// none that do any work.
//
// Everything else stays discoverable. Adding a ninth is a real decision with a real price,
// so it should require editing this list and reading this comment.
func TestOnlyTheWorkingSetIsAlwaysActive(t *testing.T) {
	t.Parallel()
	want := []string{
		"ask_user", "document_open", "document_search", "fs_read",
		"read_tool_output", "shell_exec", "text_response", "tool_search",
	}

	// Every tool the daemon can register, constructed the cheap way (no wiring): Spec() is
	// a pure function of the value, and Deferred never depends on collaborators.
	all := []Tool{
		&AskUser{}, &CurrentTime{}, &DocumentOpen{}, &DocumentSearch{}, &FSEdit{}, &FSGlob{},
		&FSGrep{}, &FSRead{}, &FSWrite{}, &ReadToolOutput{}, &SendFile{}, &ShellExec{},
		&SkillTool{}, &TaskTool{}, &TextResponse{}, &TodoTool{}, &ToolSearch{}, &WebFetch{},
		&WebSearch{},
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
