package main

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
)

// TestPromptNamesOnlyLoadedTools enforces the one rule that governs what may be
// written in the system prompt: a tool may be named there if and only if it is
// LOADED. Everything deferred is referred to by capability family.
//
// It lives here because it is the only place both halves are reachable — the
// authored prompt in internal/agent, and the LIVE registry, which package main
// assembles. A rule that could only be checked against a hand-maintained list would
// drift exactly the way the thing it guards drifted.
//
// What it guards, measured 2026-08-03: the manifest held four tools — ask_user,
// read_tool_output, text_response, tool_search — none of which does any work, while
// the prompt taught shell_exec eight times, document_search three, and seven other
// deferred tools besides. Asked for a customer code that was in the operator's own
// spreadsheet, she went to memory, then to the PUBLIC WEB, then listed the entire
// filesystem, and reached the document library on the fourth attempt.
//
// The failure is asymmetric, which is why the test is one-directional. Un-deferring
// a tool and forgetting to teach it costs a search round trip. Teaching one that is
// not in the manifest sends her looking for it somewhere else entirely.
// ambiguousToolName lists tools whose names are also ordinary English words the
// prompt uses as nouns — "never edit a skill by writing files there", "a task that
// splits into independent subtasks". A substring check cannot tell those from a
// tool reference, and widening it to word boundaries would not help: the words are
// genuinely the same. They are exempted HERE, visibly, rather than by loosening the
// rule for every tool.
//
// The exemption is safe in the direction that matters. What the check exists to stop
// is the prompt TEACHING a deferred tool — "call skill_manage action=install", "shell_exec
// is a full terminal" — and any such instruction names other tools, verbs or
// arguments alongside, which the rest of the check still catches.
var ambiguousToolName = map[string]bool{"skill": true, "task": true}

func TestPromptNamesOnlyLoadedTools(t *testing.T) {
	registry := buildRegistry()
	prompt := agent.SystemPrompt

	check := func(name string, isDeferred bool) {
		if !isDeferred || ambiguousToolName[name] {
			return
		}
		if strings.Contains(prompt, name) {
			t.Errorf("the prompt names %q, which is DEFERRED — the model is being taught a tool that is not in its manifest", name)
		}
	}

	var deferred, loaded int
	for _, entry := range registry.Render() {
		if entry.Deferred {
			deferred++
		} else {
			loaded++
		}
		check(entry.Name, entry.Deferred)
	}

	// document_open and document_search register only when a live pool exists
	// (buildBaseRegistryWithHandles), so buildRegistry — which passes a nil store —
	// does not contain them and the loop above cannot see them. They are the
	// deployment's whole point, so checking them is not optional: their Spec is a
	// pure function of the value, exactly as in TestOnlyTheWorkingSetIsAlwaysActive.
	// (document_index was removed with the hand-built pipeline: the bucket is the
	// source of truth now, so putting a file there IS the indexing action.)
	for _, tool := range []tools.Tool{&tools.DocumentOpen{}, &tools.DocumentSearch{}} {
		spec := tool.Spec()
		check(spec.Name, spec.Deferred)
	}
	if deferred == 0 || loaded == 0 {
		t.Fatalf("registry looks wrong: %d loaded, %d deferred — the check would pass vacuously", loaded, deferred)
	}

	// Anthropic's guidance for this pattern is to keep the three to five most-used
	// tools loaded so the model can act without searching first. Aura had zero that
	// do any work; ask_user, read_tool_output, text_response and tool_search only
	// ask, page, reply and search. At least one capability that DOES something must
	// be in the manifest, or every substantive turn opens with a search again.
	working := 0
	for _, name := range []string{"shell_exec", "fs_read", "document_search", "document_open"} {
		if tool, ok := registry.Get(name); ok && !tool.Spec().Deferred {
			working++
		}
	}
	if working == 0 {
		t.Error("no working capability is loaded: the manifest can only ask, page, reply and search, " +
			"so every substantive turn must open with a tool_search round trip")
	}
}
