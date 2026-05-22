package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestSearch_ActionSearch_HybridReturns verifies that action=search delegates
// to the search_memory backend and returns a non-empty result string.
func TestSearch_ActionSearch_HybridReturns(t *testing.T) {
	ctx := context.Background()
	index := newTestMemoryIndex(t)
	mem := NewSearchMemoryTool(nil, index)
	tool := NewSearchTool(mem, newTestWikiStore(t), nil)
	if tool == nil {
		t.Fatal("NewSearchTool returned nil")
	}
	out, err := tool.Execute(ctx, map[string]any{
		"action": "search",
		"query":  "contract deadline",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Empty index → "No memory found" is a valid delegated result.
	if out == "" {
		t.Fatal("expected non-empty result from search delegation")
	}
}

// TestSearch_ActionList_PrefixMatch verifies that action=list with slug_prefix
// returns only wiki slugs matching the prefix.
func TestSearch_ActionList_PrefixMatch(t *testing.T) {
	ctx := context.Background()
	s := newTestWikiStore(t)
	seedPage(t, s, "Alpha One", "body alpha", nil, nil)
	seedPage(t, s, "Alpha Two", "body alpha2", nil, nil)
	seedPage(t, s, "Beta One", "body beta", nil, nil)

	tool := NewSearchTool(nil, s, nil)
	out, err := tool.Execute(ctx, map[string]any{
		"action":      "list",
		"slug_prefix": "alpha",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result struct {
		Matches []struct {
			Slug string `json:"slug"`
			Kind string `json:"kind"`
		} `json:"matches"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(result.Matches) != 2 {
		t.Errorf("expected 2 alpha matches, got %d: %s", len(result.Matches), out)
	}
	for _, m := range result.Matches {
		if !strings.HasPrefix(m.Slug, "alpha") {
			t.Errorf("slug %q does not match prefix alpha", m.Slug)
		}
		if m.Kind != "wiki" {
			t.Errorf("slug %q has kind %q, want wiki", m.Slug, m.Kind)
		}
	}
}

// TestSearch_ActionRead_BodyReturned verifies that action=read returns the wiki
// page body and slug as JSON.
func TestSearch_ActionRead_BodyReturned(t *testing.T) {
	ctx := context.Background()
	s := newTestWikiStore(t)
	seedPage(t, s, "My Topic", "Body content here.", nil, nil)

	tool := NewSearchTool(nil, s, nil)
	out, err := tool.Execute(ctx, map[string]any{
		"action": "read",
		"slug":   "my-topic",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var result struct {
		Slug  string `json:"slug"`
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if result.Slug != "my-topic" {
		t.Errorf("slug = %q, want my-topic", result.Slug)
	}
	if result.Title != "My Topic" {
		t.Errorf("title = %q, want My Topic", result.Title)
	}
	if result.Body != "Body content here." {
		t.Errorf("body = %q, want 'Body content here.'", result.Body)
	}
}

// TestSearch_ZoneFilter verifies that zone=wiki only searches the wiki backend
// and does not surface source hits from the compact index.
func TestSearch_ZoneFilter(t *testing.T) {
	ctx := context.Background()
	// SearchMemoryTool with compact index only (wiki searcher is nil).
	// zone=wiki should route only to wiki search → unavailable warning, no source hits.
	index := newTestMemoryIndex(t)
	mem := NewSearchMemoryTool(nil, index)
	tool := NewSearchTool(mem, newTestWikiStore(t), nil)

	out, err := tool.Execute(ctx, map[string]any{
		"action": "search",
		"query":  "anything",
		"zone":   "wiki",
	})
	if err != nil {
		t.Fatalf("zone=wiki execute: %v", err)
	}
	// wiki searcher is nil → no wiki hits; result must not contain source kind hits.
	if strings.Contains(out, "[source]") {
		t.Errorf("zone=wiki result should not contain source hits:\n%s", out)
	}
}

// TestSearch_SearchMemoryInternalExecution verifies that SearchMemoryTool still
// executes without error when used as the internal delegate of SearchTool.
// search_memory is no longer registered in the LLM-facing tool catalog; it is
// an internal implementation detail of the unified "search" tool.
func TestSearch_SearchMemoryInternalExecution(t *testing.T) {
	ctx := context.Background()
	index := newTestMemoryIndex(t)
	tool := NewSearchMemoryTool(nil, index)

	out, err := tool.Execute(ctx, map[string]any{"query": "anything"})
	if err != nil {
		t.Fatalf("SearchMemoryTool.Execute (internal): %v", err)
	}
	if strings.Contains(out, "DEPRECATED") {
		t.Errorf("SearchMemoryTool output must not contain DEPRECATED hint (it is now internal-only):\n%s", out)
	}
}
