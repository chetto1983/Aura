package tools

import (
	"strings"
	"testing"
)

// 2026-05-11: embedding text follows the ToolRAG document shape
// (https://github.com/antl3x/ToolRAG) — name with underscores → spaces,
// description, then one line per parameter ("    <name> [<type>]: <desc>").
// This intentionally broadens the previous D-24 narrow form because
// terse top-line descriptions (web_fetch, web_search) embedded too far from
// multilingual queries — see the picobot regression in the same day's logs.
func TestSearchableToolEmbeddingText_ToolRAGShape(t *testing.T) {
	def := ToolDefinition{
		Name:        "web_fetch",
		Description: "Fetch a web page by URL.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "The URL to fetch.",
				},
				"timeout": map[string]any{
					"type": "integer",
				},
			},
			"required": []string{"url"},
		},
		Examples: []ToolCallExample{
			{Description: "fetch hn", Arguments: map[string]any{"url": "https://example.com"}},
		},
	}
	got := searchableToolEmbeddingText(def)

	// Tool name is the natural-language form (snake_case → spaces).
	if !strings.HasPrefix(got, "web fetch: Fetch a web page by URL.") {
		t.Fatalf("embedding header wrong: %q", got)
	}
	// Each parameter contributes a line with name, type, description.
	for _, frag := range []string{"    timeout [integer]", "    url [string]: The URL to fetch."} {
		if !strings.Contains(got, frag) {
			t.Fatalf("embedding missing %q: %q", frag, got)
		}
	}
	// Parameters appear in deterministic (alphabetical) order so the
	// embed text hashes identically across rebuilds.
	if strings.Index(got, "timeout") > strings.Index(got, "url") {
		t.Fatalf("parameters not in alphabetical order: %q", got)
	}
	// Examples / arguments still excluded — same negative invariant as D-24.
	for _, tok := range []string{"arguments", "fetch hn", "required"} {
		if strings.Contains(got, tok) {
			t.Fatalf("embedding contains forbidden token %q: %q", tok, got)
		}
	}
}

func TestToolNameForEmbeddingReplacesSeparators(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"web_fetch", "web fetch"},
		{"mcp_mail_imap_search_messages", "mcp mail imap search messages"},
		{"some-dashed-tool", "some dashed tool"},
		{"plain", "plain"},
	} {
		if got := toolNameForEmbedding(tc.in); got != tc.want {
			t.Errorf("toolNameForEmbedding(%q) = %q, want %q", tc.in, got, tc.want)
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
