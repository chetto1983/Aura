package tools

import (
	"strings"
	"testing"
)

func TestSearchableToolEmbeddingText_NameAndDescriptionOnly(t *testing.T) {
	def := ToolDefinition{
		Name:        "frobnicate",
		Description: "Frobnicate a thing.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"x": map[string]any{"type": "string"},
			},
			"required": []string{"x"},
		},
		Examples: []ToolCallExample{
			{Description: "frob", Arguments: map[string]any{"x": "y"}},
		},
	}
	got := searchableToolEmbeddingText(def)
	want := "frobnicate frobnicate a thing."
	if got != want {
		t.Fatalf("searchableToolEmbeddingText = %q, want %q", got, want)
	}
	// Negative invariants — D-24: embedding text MUST NOT carry parameters/examples/tags.
	// Note: "frob" is omitted here as it is a substring of the tool name "frobnicate";
	// the tokens below are distinct to the parameters/examples JSON blobs only.
	forbidden := []string{"properties", "required", "arguments"}
	for _, tok := range forbidden {
		if strings.Contains(got, tok) {
			t.Fatalf("embedding text contains forbidden token %q: %q", tok, got)
		}
	}
}

func TestSearchableToolText_LexKeepsBroadCorpus(t *testing.T) {
	// Sanity: the existing searchableToolText keeps the broader corpus for lex,
	// including examples and parameters JSON. Phase 2 narrows ONLY the embedding
	// text — lex search behavior must NOT change.
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

func TestToolVectorIndex_ExportedType(t *testing.T) {
	// Compile-time check: ToolVectorIndex is exported at package scope.
	var _ *ToolVectorIndex = nil
}
