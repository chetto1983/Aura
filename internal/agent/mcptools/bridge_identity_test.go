package mcptools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
)

func TestBridgedMemoryToolInjectsContextUserIdentifier(t *testing.T) {
	srv := &fakeServer{defs: []mcp.ToolDef{{Name: "memory_store_message", Description: "Store memory."}}, callText: "ok"}
	got, err := Bridge(context.Background(), "memory", srv)
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	ctx := identityctx.WithIdentityID(context.Background(), "identity-1")
	ctx = tools.WithToolCallContext(ctx, "sess", "tc1", t.TempDir(), 2048)

	_, err = got[0].Execute(ctx, json.RawMessage(`{"content":"hello","role":"user","user_identifier":"spoofed"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if srv.lastArgs["user_identifier"] != "identity-1" {
		t.Fatalf("user_identifier = %v, want authenticated identity", srv.lastArgs["user_identifier"])
	}
}

// TestBridgedMemoryToolFallsBackToOperatorWhenNoPrincipal guards the fail-open fix:
// the memory server runs an unscoped GLOBAL query when a call carries no
// user_identifier, so a no-principal (CLI/unauthenticated) memory call must be scoped
// to the seeded local operator identity rather than forwarded bare. Without this the
// call would read every tenant's memory.
func TestBridgedMemoryToolFallsBackToOperatorWhenNoPrincipal(t *testing.T) {
	srv := &fakeServer{defs: []mcp.ToolDef{{Name: "memory_search", Description: "Search memory."}}, callText: "ok"}
	got, err := Bridge(context.Background(), "memory", srv)
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	// No identityctx.WithIdentityID — the no-principal path.
	ctx := tools.WithToolCallContext(context.Background(), "sess", "tc1", t.TempDir(), 2048)

	_, err = got[0].Execute(ctx, json.RawMessage(`{"query":"anything"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if srv.lastArgs["user_identifier"] != identityctx.LocalOperatorIdentity {
		t.Fatalf("no-principal user_identifier = %v, want operator fallback %q (never unscoped)",
			srv.lastArgs["user_identifier"], identityctx.LocalOperatorIdentity)
	}
}

func TestBridgedNonMemoryToolDoesNotInjectUserIdentifier(t *testing.T) {
	srv := &fakeServer{defs: sandboxDefs(), callText: "ok"}
	got, err := Bridge(context.Background(), "sb", srv)
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	ctx := identityctx.WithIdentityID(context.Background(), "identity-1")
	ctx = tools.WithToolCallContext(ctx, "sess", "tc1", t.TempDir(), 2048)

	_, err = got[0].Execute(ctx, json.RawMessage(`{"container_id":"abc"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, ok := srv.lastArgs["user_identifier"]; ok {
		t.Fatalf("non-memory tool received user_identifier: %+v", srv.lastArgs)
	}
}
