package tools

import (
	"strings"
	"testing"

	"github.com/aura/aura/internal/wiki"
)

func TestWriteAndReadWikiTools(t *testing.T) {
	store, err := wiki.NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	write := NewWriteWikiTool(store, nil)
	if write.Name() != "write_wiki" || write.Description() == "" || write.Parameters()["type"] != "object" {
		t.Fatal("write_wiki metadata is incomplete")
	}

	result, err := write.Execute(t.Context(), map[string]any{
		"title":    "Tool Calling Notes",
		"body":     "Aura uses [[tool-calling]] for wiki writes.",
		"tags":     []any{"tools", "wiki", ""},
		"category": "engineering",
		"related":  []any{"tool-calling"},
		"sources":  []any{"https://example.com"},
	})
	if err != nil {
		t.Fatalf("write.Execute() error = %v", err)
	}
	if !strings.Contains(result, "[[tool-calling-notes]]") {
		t.Fatalf("write result = %q, want slug", result)
	}

	read := NewReadWikiTool(store)
	if read.Name() != "read_wiki" || read.Description() == "" || read.Parameters()["type"] != "object" {
		t.Fatal("read_wiki metadata is incomplete")
	}
	out, err := read.Execute(t.Context(), map[string]any{"slug": "tool-calling-notes"})
	if err != nil {
		t.Fatalf("read.Execute() error = %v", err)
	}
	for _, want := range []string{"# Tool Calling Notes", "Category: engineering", "Tags: tools, wiki", "Related: [[tool-calling]]", "Sources: https://example.com"} {
		if !strings.Contains(out, want) {
			t.Fatalf("read output missing %q: %s", want, out)
		}
	}
}

func TestWikiToolValidation(t *testing.T) {
	store, err := wiki.NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	write := NewWriteWikiTool(store, nil)
	if _, err := write.Execute(t.Context(), map[string]any{"title": "Missing Body"}); err == nil {
		t.Fatal("expected validation error for missing body")
	}

	read := NewReadWikiTool(store)
	if _, err := read.Execute(t.Context(), map[string]any{"slug": ""}); err == nil {
		t.Fatal("expected validation error for empty slug")
	}
}

func TestSearchWikiToolMetadata(t *testing.T) {
	searchTool := NewSearchWikiTool(nil)
	if searchTool.Name() != "search_wiki" || searchTool.Description() == "" || searchTool.Parameters()["type"] != "object" {
		t.Fatal("search_wiki metadata is incomplete")
	}
}
