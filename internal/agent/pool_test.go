package agent

import (
	"testing"

	"github.com/aura/aura/internal/llm"
)

func TestNewToolPool_SeedsFromInitial(t *testing.T) {
	defs := []llm.ToolDefinition{
		{Name: "search_memory", Description: "Search memory"},
		{Name: "wiki_page", Description: "Wiki page"},
	}
	pool := newToolPool(defs, nil)
	if !pool.Has("search_memory") {
		t.Fatal("search_memory missing")
	}
	if !pool.Has("wiki_page") {
		t.Fatal("wiki_page missing")
	}
	out := pool.Defs()
	if len(out) != 2 {
		t.Fatalf("Defs() len: got %d", len(out))
	}
}

func TestToolPool_IgnoresEmptyNamesInSeed(t *testing.T) {
	defs := []llm.ToolDefinition{
		{Name: "", Description: "noop"},
		{Name: "real", Description: "ok"},
	}
	pool := newToolPool(defs, nil)
	if pool.Has("") {
		t.Fatal("pool kept an empty-name def")
	}
	if !pool.Has("real") {
		t.Fatal("real missing")
	}
}

func TestToolPool_EnsureLoadedWithoutResolverReturnsFalse(t *testing.T) {
	pool := newToolPool(nil, nil)
	if pool.EnsureLoaded("missing") {
		t.Fatal("EnsureLoaded should return false without a resolver")
	}
}

func TestToolPool_EnsureLoadedFromResolver(t *testing.T) {
	resolver := func(name string) (llm.ToolDefinition, bool) {
		if name == "create_docx" {
			return llm.ToolDefinition{Name: "create_docx", Description: "Generate docx"}, true
		}
		return llm.ToolDefinition{}, false
	}
	pool := newToolPool(nil, resolver)

	if !pool.EnsureLoaded("create_docx") {
		t.Fatal("EnsureLoaded should resolve create_docx")
	}
	if !pool.Has("create_docx") {
		t.Fatal("Has should report create_docx after EnsureLoaded")
	}
	if pool.EnsureLoaded("xyzzy") {
		t.Fatal("EnsureLoaded should return false for unknown tool")
	}
}

func TestToolPool_EnsureLoadedIsIdempotent(t *testing.T) {
	calls := 0
	resolver := func(name string) (llm.ToolDefinition, bool) {
		calls++
		return llm.ToolDefinition{Name: name, Description: "ok"}, true
	}
	pool := newToolPool(nil, resolver)
	for i := 0; i < 5; i++ {
		if !pool.EnsureLoaded("x") {
			t.Fatal("EnsureLoaded should succeed")
		}
	}
	if calls != 1 {
		t.Fatalf("resolver called %d times — expected 1 (cached after first)", calls)
	}
}

func TestToolPool_NilSafe(t *testing.T) {
	var pool *toolPool
	if pool.Has("x") {
		t.Fatal("nil pool Has should be false")
	}
	if pool.EnsureLoaded("x") {
		t.Fatal("nil pool EnsureLoaded should be false")
	}
	if pool.Defs() != nil {
		t.Fatal("nil pool Defs should be nil")
	}
}

