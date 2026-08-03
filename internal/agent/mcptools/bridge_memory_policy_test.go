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
		// Raw path-specific reads remain available to the host and CLI, but the model
		// receives one deterministic read contract.
		{Name: "memory_reembed"},
		{Name: "memory_search"},
		{Name: "memory_digest"},
		{
			Name:        "memory_recall",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"},"user_identifier":{"type":"string"}}}`),
			Annotations: mcp.ToolAnnotations{ReadOnlyHint: true},
		},
	}}

	bridged := bridgeToolsWithPolicy("mem", srv, srv.defs, 0, bridgePolicy{memory: true})
	if len(bridged) != 1 {
		t.Fatalf("memory alias mounted %d tools, want only the unified recall tool", len(bridged))
	}
	recall := bridged[0]
	if got := recall.Spec().Name; got != "mem__memory_recall" {
		t.Fatalf("aliased model name = %q, want mem__memory_recall", got)
	}
	// Deferred like every other bridged server now: the memory exception cost ~2.7k
	// tokens of manifest on every turn. What this test still owns is the ALIAS and the
	// hidden-surface filter, which are unaffected.
	if !recall.Spec().Deferred {
		t.Fatal("memory alias recall must be deferred like every other bridged tool")
	}

	ctx := identityctx.WithIdentityID(context.Background(), "tenant-a")
	ctx = tools.WithToolCallContext(ctx, "session", "call", t.TempDir(), 2048)
	if _, err := recall.Execute(ctx, json.RawMessage(`{"query":"marker","user_identifier":"tenant-b"}`)); err != nil {
		t.Fatalf("execute aliased memory recall: %v", err)
	}
	if got := srv.lastArgs["user_identifier"]; got != "tenant-a" {
		t.Fatalf("forwarded user_identifier = %v, want authenticated tenant-a", got)
	}
}
