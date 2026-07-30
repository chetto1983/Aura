package mcptools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
)

func TestMemoryBridgePolicy_AliasKeepsIsolationAndHiddenSurface(t *testing.T) {
	srv := &fakeServer{defs: []mcp.ToolDef{
		{Name: "memory_store_message"},
		{
			Name:        "memory_search",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"user_identifier":{"type":"string"}}}`),
			Annotations: mcp.ToolAnnotations{ReadOnlyHint: true},
		},
	}}

	bridged := bridgeToolsWithPolicy("mem", srv, srv.defs, 0, bridgePolicy{memory: true})
	if len(bridged) != 1 {
		t.Fatalf("memory alias mounted %d tools, want only the model-facing search tool", len(bridged))
	}
	search := bridged[0]
	if got := search.Spec().Name; got != "mem__memory_search" {
		t.Fatalf("aliased model name = %q, want mem__memory_search", got)
	}
	// Deferred like every other bridged server now: the memory exception cost ~2.7k
	// tokens of manifest on every turn. What this test still owns is the ALIAS and the
	// hidden-surface filter, which are unaffected.
	if !search.Spec().Deferred {
		t.Fatal("memory alias search must be deferred like every other bridged tool")
	}

	ctx := identityctx.WithIdentityID(context.Background(), "tenant-a")
	ctx = tools.WithToolCallContext(ctx, "session", "call", t.TempDir(), 2048)
	if _, err := search.Execute(ctx, json.RawMessage(`{"query":"marker","user_identifier":"tenant-b"}`)); err != nil {
		t.Fatalf("execute aliased memory search: %v", err)
	}
	if got := srv.lastArgs["user_identifier"]; got != "tenant-a" {
		t.Fatalf("forwarded user_identifier = %v, want authenticated tenant-a", got)
	}
}
