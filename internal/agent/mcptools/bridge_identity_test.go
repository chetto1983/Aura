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

func TestBridgedMemoryGraphQueryInjectsScopeForServerSideRejection(t *testing.T) {
	srv := &fakeServer{defs: []mcp.ToolDef{{Name: "graph_query", Description: "Read graph."}}, callText: "ok"}
	got, err := Bridge(context.Background(), "memory", srv)
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	ctx := identityctx.WithIdentityID(context.Background(), "identity-1")
	ctx = tools.WithToolCallContext(ctx, "sess", "tc1", t.TempDir(), 2048)

	_, err = got[0].Execute(ctx, json.RawMessage(`{"query":"MATCH (n) RETURN n LIMIT 1"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if srv.lastArgs["user_identifier"] != "identity-1" {
		t.Fatalf("graph_query user_identifier = %v, want authenticated identity for scoped rejection", srv.lastArgs["user_identifier"])
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
