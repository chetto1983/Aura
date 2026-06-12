package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestRenderToolDefs asserts RenderToolDefs returns []llm.ToolDef in the same
// alphabetical order as Render(), one per tool, with Name/Description/Parameters
// carried (deferred tools fall back to Summary and carry no Parameters).
func TestRenderToolDefs(t *testing.T) {
	r := NewRegistry()
	// Registered out of alphabetical order on purpose.
	r.Register(zebraTool{})
	r.Register(alphaTool{})
	r.Register(deferredMidTool{})

	entries := r.Render()
	defs := r.RenderToolDefs()

	if len(defs) != len(entries) {
		t.Fatalf("RenderToolDefs len = %d, Render len = %d — must be 1:1", len(defs), len(entries))
	}
	// Same alphabetical order as Render (no re-sort).
	for i := range entries {
		if defs[i].Function.Name != entries[i].Name {
			t.Errorf("def[%d].Name = %q, Render[%d].Name = %q — order diverged", i, defs[i].Function.Name, i, entries[i].Name)
		}
		if defs[i].Type != "function" {
			t.Errorf("def[%d].Type = %q, want function", i, defs[i].Type)
		}
	}
	// Names are alpha-sorted: alpha, mid_deferred, zebra.
	want := []string{"alpha", "mid_deferred", "zebra"}
	for i, w := range want {
		if defs[i].Function.Name != w {
			t.Errorf("defs[%d].Name = %q, want %q", i, defs[i].Function.Name, w)
		}
	}

	// Non-deferred tool carries its full Description + Parameters.
	alpha := defs[0]
	if alpha.Function.Description != "alpha full description" {
		t.Errorf("alpha Description = %q, want full description", alpha.Function.Description)
	}
	if len(alpha.Function.Parameters) == 0 {
		t.Error("alpha Parameters empty, want the JSON schema carried")
	}

	// Deferred tool falls back to Summary and carries no Parameters (hidden until
	// tool_search promotes it).
	mid := defs[1]
	if mid.Function.Description != "mid summary" {
		t.Errorf("deferred Description = %q, want the Summary fallback", mid.Function.Description)
	}
	if len(mid.Function.Parameters) != 0 {
		t.Errorf("deferred Parameters = %q, want empty (hidden until tool_search)", mid.Function.Parameters)
	}
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
	for _, d := range r.RenderToolDefs() {
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
		name        string
		tool        Tool
		wantName    string
		wantSummary string
		wantPhrases []string
		wantMutate  bool
	}{
		{
			name:        "read",
			tool:        &FSRead{},
			wantName:    "fs_read",
			wantSummary: "Read a file from disk.",
			wantPhrases: []string{"1-based `offset`", "Read a file with this tool BEFORE editing", "large result pages"},
		},
		{
			name:        "edit",
			tool:        &FSEdit{},
			wantName:    "fs_edit",
			wantSummary: "Replace an exact string in a file.",
			wantPhrases: []string{"Read the file first", "must be UNIQUE", "`replace_all`"},
			wantMutate:  true,
		},
		{
			name:        "write",
			tool:        &FSWrite{},
			wantName:    "fs_write",
			wantSummary: "Write a file to disk (create or overwrite).",
			wantPhrases: []string{"COMPLETE `content`", "prefer fs_edit", "Always report the absolute path"},
			wantMutate:  true,
		},
		{
			name:        "glob",
			tool:        &FSGlob{},
			wantName:    "fs_glob",
			wantSummary: "Find files by name pattern.",
			wantPhrases: []string{"Find files by NAME", "`**`", "use fs_grep to search their contents"},
		},
		{
			name:        "grep",
			tool:        &FSGrep{},
			wantName:    "fs_grep",
			wantSummary: "Search file contents with a regexp.",
			wantPhrases: []string{"Search file CONTENTS", "RE2 regular expression", "To find files by NAME use fs_glob"},
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
			if spec.Deferred {
				t.Fatal("filesystem tools must be visible without deferred discovery")
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
