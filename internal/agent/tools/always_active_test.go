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
//   - read_file       — reading a file whose path she already has is not a choice between
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
// Everything else stays discoverable. Adding one more is a real decision with a real price,
// so it should require editing this list and reading this comment.
//
// 2026-08-07: the set was aligned to the reference coding agent's own always-active tools,
// which is a measured configuration rather than an opinion. That added five:
//   - patch, write_file, search_files — the file toolset is ONE toolset. Splitting it left the
//     model able to read but not edit or find without first paying a tool_search on the single
//     most common path there is, which costs more turns than the manifest bytes it saved.
//   - send_file — delivering a result is the last step of most requests, not a rare one.
//
// It also confirmed two that must STAY deferred, because the reference agent defers them too:
// todo_write and web_search.
//
// Two more MATCH the reference agent's set but stay deferred here for a reason the set alone
// does not capture — Aura's descriptions are several times larger than the equivalents, and the
// manifest is billed on every turn, cold and cached:
//   - swarm_spawn: swarmSpawnDescription is 4,364 chars (~1,100 tokens).
//
// skill was on that list and has been PROMOTED, which is the outcome the note above
// anticipated ("promotion candidates once their descriptions are cut to the reference
// size"). Two cuts did it. The authoring half moved to skill_manage, and then the
// catalogue of installed skills moved out of the Description into the messages[1]
// always-block — the seam built for per-turn live state. Measured on the live registry
// with 11 skills installed: 7,091 bytes (~1,773 tokens) before, ~400 after, and the
// tools array stopped being rewritten every time a skill is added or removed. What is
// billed every turn is now a constant, and the model can see that skills exist without
// spending a tool_search round trip to find out.
//
// Both are promotion candidates once their descriptions are cut to the reference size. Matching
// the set is not enough on its own: the bytes are the other half.
func TestOnlyTheWorkingSetIsAlwaysActive(t *testing.T) {
	t.Parallel()
	want := []string{
		"ask_user", "document_open", "document_search", "patch", "read_file",
		"read_tool_output", "search_files", "send_file", "shell_exec",
		"skill",
		"text_response", "tool_search", "write_file",
	}

	// Every tool the daemon can register, constructed the cheap way (no wiring): Spec() is
	// a pure function of the value, and Deferred never depends on collaborators.
	all := []Tool{
		&AskUser{}, &CurrentTime{}, &DocumentOpen{}, &DocumentSearch{}, &Patch{}, &SearchFiles{},
		&ReadFile{}, &WriteFile{}, &ReadToolOutput{}, &SendFile{}, &ShellExec{},
		&SkillTool{}, &SwarmSpawn{}, &TaskTool{}, &TextResponse{}, &TodoTool{}, &ToolSearch{},
		&WebFetch{}, &WebSearch{},
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
