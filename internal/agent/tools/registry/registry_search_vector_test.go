package tools

import (
	"context"
	"testing"
)

func TestNewToolVectorIndexDefaults(t *testing.T) {
	idx := NewToolVectorIndex(ToolVectorConfig{}, nil)
	if idx == nil {
		t.Fatal("NewToolVectorIndex returned nil")
	}
	if idx.cfg.Backend != "fts" {
		t.Fatalf("default backend = %q, want fts", idx.cfg.Backend)
	}
	if idx.cfg.Collection != "aura_tool_search_v2" {
		t.Fatalf("default collection = %q, want aura_tool_search_v2", idx.cfg.Collection)
	}
}

func TestToolVectorIndexSearchWhenFTS(t *testing.T) {
	idx := NewToolVectorIndex(ToolVectorConfig{Backend: "fts"}, nil)
	results, err := idx.Search(context.Background(), "test query", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no results for fts backend, got %d", len(results))
	}
}

func TestToolVectorIndexNilReady(t *testing.T) {
	var idx *ToolVectorIndex
	if err := idx.Ready(context.Background()); err != nil {
		t.Fatalf("nil index Ready should not error: %v", err)
	}
}

func TestToolVectorIndexHealth(t *testing.T) {
	idx := NewToolVectorIndex(ToolVectorConfig{
		Backend:    "hybrid",
		EmbedModel: "test-model",
	}, nil)
	h := idx.Health()
	if h.Backend != "hybrid" {
		t.Fatalf("Health.Backend = %q, want hybrid", h.Backend)
	}
	if h.Fallback {
		t.Fatal("Health.Fallback = true, want false")
	}
}

func TestToolVectorIndexHealthWithError(t *testing.T) {
	idx := NewToolVectorIndex(ToolVectorConfig{Backend: "vector"}, nil)
	idx.lastError = context.DeadlineExceeded
	h := idx.Health()
	if h.LastError == "" {
		t.Fatal("expected LastError to be set")
	}
	if !h.Fallback {
		t.Fatal("expected Fallback = true when lastError is set")
	}
}

func TestToolVectorIndexNilHealth(t *testing.T) {
	var idx *ToolVectorIndex
	h := idx.Health()
	if h.Backend != "fts" {
		t.Fatalf("nil Health.Backend = %q, want fts", h.Backend)
	}
	if !h.Fallback {
		t.Fatal("nil Health.Fallback = false, want true")
	}
}

func TestToolQdrantPointID(t *testing.T) {
	id1 := ToolQdrantPointID("execute_code")
	id2 := ToolQdrantPointID("execute_code")
	id3 := ToolQdrantPointID("execute_shell")
	if id1 != id2 {
		t.Fatalf("same name produced different ids: %q vs %q", id1, id2)
	}
	if id1 == id3 {
		t.Fatal("different names produced same id")
	}
	if len(id1) != 36 {
		t.Fatalf("point id length = %d, want 36", len(id1))
	}
}

func TestToolVectorConfigDefaultCollection(t *testing.T) {
	idx := NewToolVectorIndex(ToolVectorConfig{Backend: "hybrid", Collection: ""}, nil)
	if idx.cfg.Collection != "aura_tool_search_v2" {
		t.Fatalf("Collection = %q, want aura_tool_search_v2", idx.cfg.Collection)
	}
}

func TestToolVectorConfigCustomCollection(t *testing.T) {
	idx := NewToolVectorIndex(ToolVectorConfig{
		Backend:    "vector",
		Collection: "custom_tools",
	}, nil)
	if idx.cfg.Collection != "custom_tools" {
		t.Fatalf("Collection = %q, want custom_tools", idx.cfg.Collection)
	}
}
