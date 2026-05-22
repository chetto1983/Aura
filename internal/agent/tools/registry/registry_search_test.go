package tools

import (
	"strings"
	"testing"
)

func TestSearchableToolText_LexKeepsBroadCorpus(t *testing.T) {
	// Sanity: searchableToolText keeps the broader corpus for lex,
	// including examples and parameters JSON.
	def := ToolDefinition{
		Name:        "frobnicate",
		Description: "Frobnicate a thing.",
		Parameters:  map[string]any{"type": "object"},
		Examples: []ToolCallExample{
			{Description: "frob it good", Arguments: map[string]any{"x": "y"}},
		},
	}
	tags := []string{"alpha", "beta"}
	got := searchableToolText(def, tags)
	for _, tok := range []string{"frobnicate", "alpha", "beta", "frob it good"} {
		if !strings.Contains(got, tok) {
			t.Fatalf("lex text missing %q: %q", tok, got)
		}
	}
}
