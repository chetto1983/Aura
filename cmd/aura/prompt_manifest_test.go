package main

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
)

// ambiguousToolName lists tools whose names are also ordinary English words the
// prompt uses as nouns — "never edit a skill by writing files there", "a task that
// splits into independent subtasks", and "board" inside "dashboard". A substring check cannot tell those from a
// tool reference, and widening it to word boundaries would not help: the words are
// genuinely the same. They are exempted HERE, visibly, rather than by loosening the
// rule for every tool.
//
// The exemption is safe in the direction that matters. What the check exists to stop
// is the prompt TEACHING a deferred tool — "call skill_manage action=install", "shell_exec
// is a full terminal" — and any such instruction names other tools, verbs or
// arguments alongside, which the rest of the check still catches.
var ambiguousToolName = map[string]bool{"board": true, "skill": true, "task": true}

// promptTag matches an angle-bracket tag such as <memory_context> or
// <tool_output source="swarm" ...>: a tag is a delimiter the prompt explains, not a
// tool, so it is removed before the prompt's code names are collected.
var promptTag = regexp.MustCompile(`</?[a-z_]+[^>]*>`)

// codeName matches a word the prompt can only mean as a code identifier: lowercase
// words joined by underscores.
var codeName = regexp.MustCompile(`\b[a-z]+(?:_[a-z]+)+\b`)

// manifestSpecs is every tool the daemon can put in front of the model.
//
// document_open and document_search register only when a live pool exists
// (buildBaseRegistryWithHandles), so buildRegistry — which passes a nil store — does
// not contain them. They are the deployment's whole point, so they are added here: their
// Spec is a pure function of the value, exactly as in TestOnlyTheWorkingSetIsAlwaysActive.
func manifestSpecs() []tools.Spec {
	var specs []tools.Spec
	for _, tool := range buildRegistry().All() {
		specs = append(specs, tool.Spec())
	}
	for _, tool := range []tools.Tool{&tools.DocumentOpen{}, &tools.DocumentSearch{}} {
		specs = append(specs, tool.Spec())
	}
	return specs
}

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
func TestPromptNamesOnlyLoadedTools(t *testing.T) {
	prompt := agent.SystemPrompt
	var deferred, loaded int
	for _, spec := range manifestSpecs() {
		if !spec.Deferred {
			loaded++
			continue
		}
		deferred++
		if !ambiguousToolName[spec.Name] && strings.Contains(prompt, spec.Name) {
			t.Errorf("the prompt names %q, which is DEFERRED — the model is being taught a tool that is not in its manifest", spec.Name)
		}
	}
	if deferred == 0 || loaded == 0 {
		t.Fatalf("registry looks wrong: %d loaded, %d deferred — the check would pass vacuously", loaded, deferred)
	}
}

// TestPromptCodeNamesExist closes the hole the check above leaves: it only looks for
// the names the registry HAS, so a name that matches no tool at all passes it. That is
// how fs_read survived in the prompt from 2026-08-07, when read_file replaced it, to
// 2026-10-03, when a captured live request showed the model told to call a tool absent
// from its 19-tool manifest. Every code name the prompt uses outside a tag must be a
// registered tool or an argument one of them declares.
func TestPromptCodeNamesExist(t *testing.T) {
	known := map[string]bool{}
	for _, spec := range manifestSpecs() {
		known[spec.Name] = true
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(spec.Parameters, &schema); err != nil {
			t.Fatalf("%s parameters: %v", spec.Name, err)
		}
		for arg := range schema.Properties {
			known[arg] = true
		}
	}
	prose := promptTag.ReplaceAllString(agent.SystemPrompt, "")
	names := codeName.FindAllString(prose, -1)
	if len(names) == 0 {
		t.Fatal("no code names found in the prompt — the check would pass vacuously")
	}
	for _, name := range names {
		if !known[name] {
			t.Errorf("the prompt names %q, which is neither a registered tool nor a declared argument", name)
		}
	}
}
