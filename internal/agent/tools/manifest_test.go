package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestRenderToolDefs asserts the full-promotion deferred-tool contract (Claude
// Code / OpenAI Agents parity). This is a DELIBERATE contract change from the old
// behavior, where every deferred tool appeared in the callable set with an EMPTY
// parameter schema (the footgun that made the model hallucinate arguments — e.g.
// send_file {"file":...} instead of {"path":...}). New contract:
//   - a non-deferred tool is ALWAYS emitted, with its full Description + Parameters;
//   - a deferred tool is OMITTED entirely when `activated` is nil/absent;
//   - the SAME deferred tool is EMITTED with its FULL Description + Parameters once
//     its name is in `activated` (tool_search loaded its schema);
//   - alphabetical order (cache-stability-load-bearing) is preserved.
func TestRenderToolDefs(t *testing.T) {
	r := NewRegistry()
	// Registered out of alphabetical order on purpose.
	r.Register(zebraTool{})
	r.Register(alphaTool{})
	r.Register(deferredMidTool{})

	// nil activated ⇒ the deferred tool is HIDDEN; only the two non-deferred tools
	// are callable, in alphabetical order.
	defs := r.RenderToolDefs(nil)
	gotNames := make([]string, len(defs))
	for i, d := range defs {
		gotNames[i] = d.Function.Name
		if d.Type != "function" {
			t.Errorf("def[%d].Type = %q, want function", i, d.Type)
		}
	}
	if want := []string{"alpha", "zebra"}; !equalStrings(gotNames, want) {
		t.Fatalf("nil-activated defs = %v, want %v (deferred mid_deferred omitted)", gotNames, want)
	}

	// Non-deferred tool carries its full Description + Parameters.
	alpha := defs[0]
	if alpha.Function.Name != "alpha" {
		t.Fatalf("defs[0].Name = %q, want alpha", alpha.Function.Name)
	}
	if alpha.Function.Description != "alpha full description" {
		t.Errorf("alpha Description = %q, want full description", alpha.Function.Description)
	}
	if len(alpha.Function.Parameters) == 0 {
		t.Error("alpha Parameters empty, want the JSON schema carried")
	}

	// Promote the deferred tool: it now appears WITH its full Description + Parameters,
	// slotted into alphabetical order (alpha, mid_deferred, zebra).
	promoted := r.RenderToolDefs(map[string]struct{}{"mid_deferred": {}})
	promotedNames := make([]string, len(promoted))
	for i, d := range promoted {
		promotedNames[i] = d.Function.Name
	}
	if want := []string{"alpha", "mid_deferred", "zebra"}; !equalStrings(promotedNames, want) {
		t.Fatalf("promoted defs = %v, want %v", promotedNames, want)
	}
	mid := promoted[1]
	if mid.Function.Description != "mid full hidden" {
		t.Errorf("promoted deferred Description = %q, want the FULL description (not the Summary fallback)", mid.Function.Description)
	}
	if len(mid.Function.Parameters) == 0 {
		t.Error("promoted deferred Parameters empty, want the full JSON schema carried once loaded")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestRenderToolDefs_Namespaced proves the alphabetical Render ordering survives
// the "__" namespaced names introduced by MCP namespacing (08.1-03): a
// "github__create_issue" sorts before a built-in "web_fetch" purely by byte order,
// with no special handling of the "__" delimiter. Render order is the cache-stability
// invariant (manifest.go:39) — a reshuffle after the rename would bust the prompt
// cache. This is a SEPARATE assertion from the dynamic-Description change so a Render
// regression is isolated. Kills any mutant that re-sorts or special-cases namespaced
// names.
func TestRenderToolDefs_Namespaced(t *testing.T) {
	r := NewRegistry()
	r.Register(namedTool{name: "web_fetch"})
	r.Register(namedTool{name: "github__create_issue"})
	r.Register(namedTool{name: "github__list_prs"})

	got := make([]string, 0, 3)
	for _, d := range r.RenderToolDefs(nil) {
		got = append(got, d.Function.Name)
	}
	want := []string{"github__create_issue", "github__list_prs", "web_fetch"}
	if len(got) != len(want) {
		t.Fatalf("RenderToolDefs len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("order[%d] = %q, want %q (namespaced names must sort alphabetically)", i, got[i], want[i])
		}
	}
}

func TestFilesystemToolSpecsDescribeOperationalContracts(t *testing.T) {
	tests := []struct {
		name         string
		tool         Tool
		wantName     string
		wantSummary  string
		wantPhrases  []string
		wantMutate   bool
		wantDeferred bool
	}{
		{
			name:         "read",
			tool:         &FSRead{},
			wantName:     "fs_read",
			wantSummary:  "Read a file from disk.",
			wantPhrases:  []string{"1-based `offset`", "Read a file with this tool BEFORE editing", "large result pages"},
			wantDeferred: true,
		},
		{
			name:         "edit",
			tool:         &FSEdit{},
			wantName:     "fs_edit",
			wantSummary:  "Replace an exact string in a file.",
			wantPhrases:  []string{"Read the file first", "must be UNIQUE", "`replace_all`"},
			wantMutate:   true,
			wantDeferred: true,
		},
		{
			name:         "write",
			tool:         &FSWrite{},
			wantName:     "fs_write",
			wantSummary:  "Write a file to disk (create or overwrite).",
			wantPhrases:  []string{"COMPLETE `content`", "prefer fs_edit", "Always report the absolute path"},
			wantMutate:   true,
			wantDeferred: true,
		},
		{
			name:         "glob",
			tool:         &FSGlob{},
			wantName:     "fs_glob",
			wantSummary:  "Find files by name pattern.",
			wantPhrases:  []string{"Find files by NAME", "`**`", "use fs_grep to search their contents"},
			wantDeferred: true,
		},
		{
			name:         "grep",
			tool:         &FSGrep{},
			wantName:     "fs_grep",
			wantSummary:  "Find text inside file contents with a regexp (grep).",
			wantPhrases:  []string{"Search file CONTENTS", "RE2 regular expression", "To find files by NAME use fs_glob"},
			wantDeferred: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := tt.tool.Spec()
			if spec.Name != tt.wantName {
				t.Fatalf("Name = %q, want %q", spec.Name, tt.wantName)
			}
			if spec.Summary != tt.wantSummary {
				t.Fatalf("Summary = %q, want %q", spec.Summary, tt.wantSummary)
			}
			if spec.Deferred != tt.wantDeferred {
				// All five are deferred now. fs_read/fs_write used to be exempt by operator
				// directive, and the operator reversed it once the bill was on the table:
				// keeping two of the five visible did not help discovery, it just made those
				// two the default answer for jobs the other three were better at, at 560
				// tokens a turn. The whole always-active budget is pinned in
				// TestOnlyFourToolsAreAlwaysActive.
				t.Fatalf("Deferred = %v, want %v", spec.Deferred, tt.wantDeferred)
			}
			if spec.Mutating != tt.wantMutate {
				t.Fatalf("Mutating = %v, want %v", spec.Mutating, tt.wantMutate)
			}
			if len(spec.Parameters) == 0 || !json.Valid(spec.Parameters) {
				t.Fatalf("Parameters are not valid JSON: %s", spec.Parameters)
			}
			for _, phrase := range tt.wantPhrases {
				if !strings.Contains(spec.Description, phrase) {
					t.Fatalf("Description for %s missing %q: %s", tt.wantName, phrase, spec.Description)
				}
			}
		})
	}
}

// namedTool is a minimal non-deferred fixture parameterized only by Name, for the
// namespaced ordering assertion.
type namedTool struct{ name string }

func (n namedTool) Spec() Spec {
	return Spec{
		Name: n.name, Summary: "s " + n.name, Description: "d " + n.name,
		Parameters: json.RawMessage(`{"type":"object"}`), Deferred: false,
	}
}
func (namedTool) Execute(context.Context, json.RawMessage) (ToolResult, error) {
	return ToolResult{}, nil
}

type alphaTool struct{}

func (alphaTool) Spec() Spec {
	return Spec{
		Name: "alpha", Summary: "alpha summary", Description: "alpha full description",
		Parameters: json.RawMessage(`{"type":"object"}`), Deferred: false,
	}
}
func (alphaTool) Execute(context.Context, json.RawMessage) (ToolResult, error) {
	return ToolResult{}, nil
}

type zebraTool struct{}

func (zebraTool) Spec() Spec {
	return Spec{Name: "zebra", Summary: "zebra summary", Description: "zebra desc", Deferred: false}
}
func (zebraTool) Execute(context.Context, json.RawMessage) (ToolResult, error) {
	return ToolResult{}, nil
}

type deferredMidTool struct{}

func (deferredMidTool) Spec() Spec {
	return Spec{
		Name: "mid_deferred", Summary: "mid summary", Description: "mid full hidden",
		Parameters: json.RawMessage(`{"type":"object"}`), Deferred: true,
	}
}
func (deferredMidTool) Execute(context.Context, json.RawMessage) (ToolResult, error) {
	return ToolResult{}, nil
}
