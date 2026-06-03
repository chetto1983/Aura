package mcptools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
)

// fakeServer is an in-memory Server: it records the last CallTool and returns
// scripted ListTools/CallTool results, so the bridge is tested without a process.
type fakeServer struct {
	defs     []mcp.ToolDef
	listErr  error
	callText string
	callErr  error
	lastName string
	lastArgs map[string]any
}

func (f *fakeServer) ListTools(context.Context) ([]mcp.ToolDef, error) { return f.defs, f.listErr }
func (f *fakeServer) CallTool(_ context.Context, name string, args map[string]any) (string, error) {
	f.lastName, f.lastArgs = name, args
	return f.callText, f.callErr
}

func sandboxDefs() []mcp.ToolDef {
	return []mcp.ToolDef{
		{
			Name:        "sandbox_exec",
			Description: "Execute commands in the sandboxed environment. \nRuns one or more shell commands.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"container_id":{"type":"string"}},"required":["container_id"]}`),
		},
		{Name: "sandbox_stop", Description: "Stop and remove a container.", InputSchema: nil},
	}
}

func TestBridge_TranslatesTools(t *testing.T) {
	got, err := Bridge(context.Background(), &fakeServer{defs: sandboxDefs()})
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 bridged tools, got %d", len(got))
	}
	exec := got[0].Spec()
	if exec.Name != "sandbox_exec" {
		t.Fatalf("name = %q", exec.Name)
	}
	if exec.Summary != "Execute commands in the sandboxed environment." {
		t.Fatalf("summary should be the first line, got %q", exec.Summary)
	}
	if exec.Deferred {
		t.Fatal("bridged tools must NOT be Deferred — the model needs the arg schema in the manifest to call them")
	}
	// inputSchema passes through unchanged.
	if !json.Valid(exec.Parameters) || !strings.Contains(string(exec.Parameters), "container_id") {
		t.Fatalf("parameters should pass the server schema through, got %s", exec.Parameters)
	}
	// A tool with no inputSchema gets the empty-object fallback (valid JSON-Schema).
	stop := got[1].Spec()
	if string(stop.Parameters) != `{"type":"object"}` {
		t.Fatalf("empty inputSchema fallback = %s", stop.Parameters)
	}
}

func TestBridgedTool_Execute_RoutesAndWraps(t *testing.T) {
	srv := &fakeServer{defs: sandboxDefs(), callText: "42"}
	got, _ := Bridge(context.Background(), srv)
	ctx := tools.WithToolCallContext(context.Background(), "sess", "tc1", t.TempDir(), 2048)

	res, err := got[0].Execute(ctx, json.RawMessage(`{"container_id":"abc","commands":["python -c \"print(40+2)\""]}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Preview != "42" {
		t.Fatalf("preview = %q, want 42", res.Preview)
	}
	if srv.lastName != "sandbox_exec" {
		t.Fatalf("routed to %q", srv.lastName)
	}
	if srv.lastArgs["container_id"] != "abc" {
		t.Fatalf("args not forwarded: %+v", srv.lastArgs)
	}
}

func TestBridgedTool_Execute_ErrorAsContent(t *testing.T) {
	srv := &fakeServer{defs: sandboxDefs(), callErr: context.DeadlineExceeded}
	got, _ := Bridge(context.Background(), srv)
	ctx := tools.WithToolCallContext(context.Background(), "sess", "tc1", t.TempDir(), 2048)

	res, err := got[0].Execute(ctx, json.RawMessage(`{"container_id":"abc"}`))
	if err != nil {
		t.Fatalf("a tool-level failure must be content, not a Go error; got %v", err)
	}
	if !strings.HasPrefix(res.Preview, "error: ") {
		t.Fatalf("preview should carry the error as content, got %q", res.Preview)
	}
}

func TestMount_RegistersAndRefusesCollision(t *testing.T) {
	reg := tools.NewRegistry()
	srv := &fakeServer{defs: sandboxDefs()}

	names, err := Mount(context.Background(), reg, srv)
	if err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("want 2 registered, got %v", names)
	}
	if _, ok := reg.Get("sandbox_exec"); !ok {
		t.Fatal("sandbox_exec not registered")
	}

	// Mounting the same server again collides on existing names — all-or-nothing.
	if _, err := Mount(context.Background(), reg, srv); err == nil {
		t.Fatal("want collision error on re-mount, got nil")
	}
}

func TestMount_RefusesDuplicateWithinServer(t *testing.T) {
	reg := tools.NewRegistry()
	srv := &fakeServer{defs: []mcp.ToolDef{
		{Name: "dup", Description: "a"},
		{Name: "dup", Description: "b"},
	}}
	if _, err := Mount(context.Background(), reg, srv); err == nil {
		t.Fatal("want duplicate-name error, got nil")
	}
	if _, ok := reg.Get("dup"); ok {
		t.Fatal("nothing should be registered when the batch has a duplicate")
	}
}
