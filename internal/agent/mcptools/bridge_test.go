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
	got, err := Bridge(context.Background(), "sb", &fakeServer{defs: sandboxDefs()}, nil)
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 bridged tools, got %d", len(got))
	}
	exec := got[0].Spec()
	if exec.Name != "sb__sandbox_exec" {
		t.Fatalf("name = %q, want namespaced sb__sandbox_exec", exec.Name)
	}
	// Summary = first description line + required-args hint: the deferred stub is
	// all the model sees by default, and without the arg names a first call has to
	// guess the shape, fail validation, tool_search, and retry (live swarm E2E
	// 2026-06-04 — each fresh worker context paid that round-trip on send_message).
	if exec.Summary != "Execute commands in the sandboxed environment. Required args: container_id." {
		t.Fatalf("summary should be first line + required-args hint, got %q", exec.Summary)
	}
	if !exec.Deferred {
		t.Fatal("bridged tools must be Deferred:true (D-20) — a multi-tool MCP server floods the manifest into the 30-50-tool degradation zone; tool_search loads the full spec on demand")
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

func TestBridge_Namespaced(t *testing.T) {
	srv := &fakeServer{defs: []mcp.ToolDef{{Name: "create_issue", Description: "Open an issue."}}}
	got, err := Bridge(context.Background(), "github", srv, nil)
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	// Model-facing name is namespaced <ns>__<tool>.
	if got[0].Spec().Name != "github__create_issue" {
		t.Fatalf("model-facing name = %q, want github__create_issue", got[0].Spec().Name)
	}
}

func TestBridgedTool_Execute_RoutesAndWraps(t *testing.T) {
	srv := &fakeServer{defs: sandboxDefs(), callText: "42"}
	got, _ := Bridge(context.Background(), "sb", srv, nil)
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

// TestBridgedTool_RoutesRawName guards the wire-name/model-name separation: even
// though the model-facing spec.Name is namespaced, Execute must route CallTool by
// the RAW server-side tool name. Kills the "route by spec.Name" mutant.
func TestBridgedTool_RoutesRawName(t *testing.T) {
	srv := &fakeServer{defs: sandboxDefs(), callText: "ok"}
	got, _ := Bridge(context.Background(), "sb", srv, nil)
	if got[0].Spec().Name != "sb__sandbox_exec" {
		t.Fatalf("precondition: model name not namespaced, got %q", got[0].Spec().Name)
	}
	ctx := tools.WithToolCallContext(context.Background(), "sess", "tc1", t.TempDir(), 2048)
	if _, err := got[0].Execute(ctx, json.RawMessage(`{"container_id":"abc"}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if srv.lastName != "sandbox_exec" {
		t.Fatalf("Execute routed by %q, want the RAW wire name sandbox_exec", srv.lastName)
	}
}

func TestBridgedTool_Execute_ErrorAsContent(t *testing.T) {
	srv := &fakeServer{defs: sandboxDefs(), callErr: context.DeadlineExceeded}
	got, _ := Bridge(context.Background(), "sb", srv, nil)
	ctx := tools.WithToolCallContext(context.Background(), "sess", "tc1", t.TempDir(), 2048)

	res, err := got[0].Execute(ctx, json.RawMessage(`{"container_id":"abc"}`))
	if err != nil {
		t.Fatalf("a tool-level failure must be content, not a Go error; got %v", err)
	}
	if !strings.HasPrefix(res.Preview, "error: ") {
		t.Fatalf("preview should carry the error as content, got %q", res.Preview)
	}
}

func TestMount_Namespaced(t *testing.T) {
	reg := tools.NewRegistry()
	srv := &fakeServer{defs: sandboxDefs()}

	names, err := Mount(context.Background(), reg, "sb", srv, nil)
	if err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("want 2 registered, got %v", names)
	}
	// Tools register under their namespaced names.
	if _, ok := reg.Get("sb__sandbox_exec"); !ok {
		t.Fatal("sb__sandbox_exec not registered")
	}
	// The raw (un-namespaced) name must NOT be registered. Kills the "register raw name" mutant.
	if _, ok := reg.Get("sandbox_exec"); ok {
		t.Fatal("raw sandbox_exec must not be registered — only the namespaced name")
	}

	// Re-mounting the same namespace collides on existing names — all-or-nothing.
	if _, err := Mount(context.Background(), reg, "sb", srv, nil); err == nil {
		t.Fatal("want collision error on re-mount, got nil")
	}
}

func TestMount_RefusesDuplicateWithinServer(t *testing.T) {
	reg := tools.NewRegistry()
	srv := &fakeServer{defs: []mcp.ToolDef{
		{Name: "dup", Description: "a"},
		{Name: "dup", Description: "b"},
	}}
	if _, err := Mount(context.Background(), reg, "srv", srv, nil); err == nil {
		t.Fatal("want duplicate-name error, got nil")
	}
	if _, ok := reg.Get("srv__dup"); ok {
		t.Fatal("nothing should be registered when the batch has a duplicate")
	}
}

// TestMount_CollisionHash exercises residual collision disambiguation: two distinct
// raw tool names that sanitize to the SAME namespaced name must not be dropped —
// the second gets a deterministic hash suffix so both register, then the
// all-or-nothing outer guard sees no remaining duplicate.
func TestMount_CollisionHash(t *testing.T) {
	reg := tools.NewRegistry()
	// "a.b" and "a/b" both sanitize to "a_b" → namespaced "srv__a_b" collides.
	srv := &fakeServer{defs: []mcp.ToolDef{
		{Name: "a.b", Description: "first"},
		{Name: "a/b", Description: "second"},
	}}
	names, err := Mount(context.Background(), reg, "srv", srv, nil)
	if err != nil {
		t.Fatalf("Mount with sanitize-collision must disambiguate, got %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("both tools must register after disambiguation, got %v", names)
	}
	// Exactly one keeps the plain namespaced name; the other carries a hash suffix.
	plain := 0
	for _, n := range names {
		if n == "srv__a_b" {
			plain++
		}
		if !strings.HasPrefix(n, "srv__a_b") {
			t.Errorf("disambiguated name should keep the colliding base prefix, got %q", n)
		}
	}
	if plain != 1 {
		t.Fatalf("exactly one tool keeps the plain name srv__a_b, got %d of %v", plain, names)
	}
	// Both names are actually registered and distinct.
	if names[0] == names[1] {
		t.Fatalf("disambiguation failed: both names equal %q", names[0])
	}
	for _, n := range names {
		if _, ok := reg.Get(n); !ok {
			t.Errorf("registered name %q not retrievable", n)
		}
	}
}

// TestMount_CollisionHash_RespectsCap covers WR-02: when two distinct raw tool names
// sanitize to the SAME namespaced base that already sits near the 64-byte cap, the
// collision-hash append must re-truncate before adding the 13-byte suffix. A bare +=
// would yield a 76-byte name; both disambiguated names must stay <= maxToolNameLen,
// distinct, and retrievable.
func TestMount_CollisionHash_RespectsCap(t *testing.T) {
	reg := tools.NewRegistry()
	// body 57 + delimiter "srv__" (5) = 62; +1 sanitized trailing char = 63-byte base.
	// "a..a/" and "a..a." both sanitize to the identical 63-byte "srv__aa..a_".
	body := strings.Repeat("a", 57)
	srv := &fakeServer{defs: []mcp.ToolDef{
		{Name: body + "/", Description: "first"},
		{Name: body + ".", Description: "second"},
	}}
	names, err := Mount(context.Background(), reg, "srv", srv, nil)
	if err != nil {
		t.Fatalf("Mount with near-cap sanitize-collision must disambiguate, got %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("both tools must register, got %v", names)
	}
	if names[0] == names[1] {
		t.Fatalf("disambiguation failed: both names equal %q", names[0])
	}
	for _, n := range names {
		if len(n) > maxToolNameLen {
			t.Errorf("disambiguated name exceeds the cap: %q len=%d (WR-02)", n, len(n))
		}
		if _, ok := reg.Get(n); !ok {
			t.Errorf("registered name %q not retrievable", n)
		}
	}
}
