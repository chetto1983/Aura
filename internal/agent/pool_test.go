package agent

import (
	"sort"
	"testing"

	"github.com/aura/aura/internal/llm"
)

func TestNewToolPool_SeedsFromInitial(t *testing.T) {
	defs := []llm.ToolDefinition{
		{Name: "tool_search", Description: "Search tools"},
		{Name: "search_memory", Description: "Search memory"},
	}
	pool := newToolPool(defs, nil)
	if !pool.Has("tool_search") {
		t.Fatal("tool_search missing")
	}
	if !pool.Has("search_memory") {
		t.Fatal("search_memory missing")
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

func TestToolPool_AbsorbToolSearchResult(t *testing.T) {
	resolver := func(name string) (llm.ToolDefinition, bool) {
		return llm.ToolDefinition{Name: name, Description: "stub"}, true
	}
	pool := newToolPool(nil, resolver)

	result := `{"query":"file word","hits":[
		{"name":"create_docx","description":"Generate docx"},
		{"name":"create_pdf","description":"Generate pdf"}
	]}`
	loaded := pool.AbsorbToolSearchResult(result)
	if loaded != 2 {
		t.Fatalf("expected 2 loaded, got %d", loaded)
	}
	if !pool.Has("create_docx") || !pool.Has("create_pdf") {
		t.Fatalf("absorption failed: pool=%v", poolNames(pool))
	}
}

func TestToolPool_AbsorbHandlesNonJSON(t *testing.T) {
	pool := newToolPool(nil, func(string) (llm.ToolDefinition, bool) {
		return llm.ToolDefinition{Name: "x"}, true
	})
	if n := pool.AbsorbToolSearchResult("No tools matched 'xyz'"); n != 0 {
		t.Fatalf("prose result should absorb 0 tools, got %d", n)
	}
	if n := pool.AbsorbToolSearchResult(""); n != 0 {
		t.Fatalf("empty result should absorb 0 tools, got %d", n)
	}
	if n := pool.AbsorbToolSearchResult("{malformed"); n != 0 {
		t.Fatalf("malformed json should absorb 0 tools, got %d", n)
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
	if pool.AbsorbToolSearchResult("{}") != 0 {
		t.Fatal("nil pool Absorb should be 0")
	}
	if pool.Defs() != nil {
		t.Fatal("nil pool Defs should be nil")
	}
}

func poolNames(p *toolPool) []string {
	defs := p.Defs()
	out := make([]string, 0, len(defs))
	for _, d := range defs {
		out = append(out, d.Name)
	}
	sort.Strings(out)
	return out
}
