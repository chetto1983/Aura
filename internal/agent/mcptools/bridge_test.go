package mcptools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/agent/tools"
)

// sandboxTools mirrors the pre-SDK fixture's two-tool shape: one tool with a
// required-arg schema, one advertising the trivial empty-object schema (a real
// AddTool call requires SOME schema — the "server sent nothing" fallback is
// covered directly against schemaJSON in bridge_edges_test.go).
func sandboxTools() []*sdkmcp.Tool {
	return []*sdkmcp.Tool{
		mustTool("sandbox_exec",
			"Execute commands in the sandboxed environment. \nRuns one or more shell commands.",
			map[string]any{
				"type":       "object",
				"properties": map[string]any{"container_id": map[string]any{"type": "string"}},
				"required":   []any{"container_id"},
			}, nil),
		mustTool("sandbox_stop", "Stop and remove a container.", nil, nil),
	}
}

func TestBridge_TranslatesTools(t *testing.T) {
	// D-27 (bridge_deferral.go): a 2-tool server is <= maxAlwaysLoadedMCPTools,
	// so on a fresh global slot budget it now earns an always-loaded slot
	// instead of the pre-amendment #123 unconditional Deferred:true. Reset so
	// this assertion doesn't depend on how many slots other tests in this
	// package already spent.
	resetLoadedSlotBudgetForTest()
	srv, _ := newInMemoryMounted(t, sandboxTools()...)
	got, err := Bridge(context.Background(), "sb", srv)
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
	if strings.Contains(strings.ToLower(exec.Summary), "untrusted") ||
		!strings.Contains(exec.Summary, "Execute commands in the sandboxed environment.") ||
		!strings.Contains(exec.Summary, "Required args: container_id.") {
		t.Fatalf("summary should carry plain text plus required-args hint, got %q", exec.Summary)
	}
	if exec.Deferred {
		t.Fatal("a 2-tool server (<= the 3-tool ceiling) on a fresh budget must earn an always-loaded slot: Deferred must be false (D-27)")
	}
	if !json.Valid(exec.Parameters) || !strings.Contains(string(exec.Parameters), "container_id") {
		t.Fatalf("parameters should pass the server schema through, got %s", exec.Parameters)
	}
	stop := got[1].Spec()
	if string(stop.Parameters) != `{"type":"object"}` {
		t.Fatalf("empty-schema tool parameters = %s", stop.Parameters)
	}
}

// TestBridge_MemoryNamespaceToolsAreDeferredByDefault fixtures the REAL memory
// tool surface (cmd/arcadedb-mcp's 10 tool names), not a 2-tool stand-in: after
// D-27, whether a mount stays deferred depends on its model-facing COUNT, and
// only the real 4-model-facing-tool shape (memory_merge_entities, memory_forget,
// memory_upsert_fact, memory_recall; the other 6 are memoryHiddenFromModel) can
// prove memory legitimately stays deferred (4 > maxAlwaysLoadedMCPTools). A
// 2-tool fixture would now qualify for a slot and assert the wrong thing.
func TestBridge_MemoryNamespaceToolsAreDeferredByDefault(t *testing.T) {
	resetLoadedSlotBudgetForTest()
	srv, _ := newInMemoryMounted(t,
		mustTool("memory_recall", "Recall memory.", nil, &sdkmcp.ToolAnnotations{ReadOnlyHint: true}),
		mustTool("memory_upsert_fact", "Store a durable fact.", nil, nil),
		mustTool("memory_merge_entities", "Merge two entities.", nil, nil),
		mustTool("memory_forget", "Forget a fact.", nil, nil),
		mustTool("graph_schema", "Describe the graph schema.", nil, &sdkmcp.ToolAnnotations{ReadOnlyHint: true}),
		mustTool("memory_search", "Search memory.", nil, &sdkmcp.ToolAnnotations{ReadOnlyHint: true}),
		mustTool("memory_facts_about", "Facts about an entity.", nil, &sdkmcp.ToolAnnotations{ReadOnlyHint: true}),
		mustTool("memory_digest", "Digest recent memory.", nil, &sdkmcp.ToolAnnotations{ReadOnlyHint: true}),
		mustTool("memory_entities", "List known entities.", nil, &sdkmcp.ToolAnnotations{ReadOnlyHint: true}),
		mustTool("memory_reembed", "Re-embed stored vectors.", nil, nil),
	)
	got, err := Bridge(context.Background(), "memory", srv)
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	// 10 advertised, 6 hidden by bridgePolicy.modelFacing (memoryHiddenFromModel):
	// only the 4 model-facing tools bridge at all.
	if len(got) != 4 {
		t.Fatalf("want 4 model-facing memory tools bridged, got %d", len(got))
	}
	for _, tool := range got {
		if !tool.Spec().Deferred {
			t.Fatalf("%s Deferred = false; memory's 4 model-facing tools exceed the 3-tool ceiling and must stay deferred (D-27)", tool.Spec().Name)
		}
	}
}

func TestBridgedToolExecuteAppliesConfiguredCallTimeout(t *testing.T) {
	t.Setenv(envMCPCallTimeoutSec, "0.025")
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
	started := make(chan struct{})
	done := make(chan struct{})
	server.AddTool(sandboxTools()[0], func(ctx context.Context, _ *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		close(started)
		defer close(done)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	srv := NewMountedServer("fixture", nil)
	session, err := connectClient(ctx, clientTransport, mcpSessionOptionsFor(srv))
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	srv.Attach(session)

	got, err := Bridge(ctx, "sb", srv)
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	callCtx := tools.WithToolCallContext(context.Background(), "sess", "tc1", t.TempDir(), 2048)

	start := time.Now()
	_, execErr := got[0].Execute(callCtx, json.RawMessage(`{"container_id":"abc"}`))
	elapsed := time.Since(start)
	if execErr == nil || !strings.Contains(execErr.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("Execute error = %v, want propagated deadline exceeded", execErr)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("configured call timeout was not applied; elapsed=%v", elapsed)
	}
	select {
	case <-started:
	default:
		t.Fatal("server handler was not reached")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("server handler did not observe context cancellation")
	}
}

func TestBridgedTool_Execute_RoutesAndWraps(t *testing.T) {
	srv, _ := newInMemoryMounted(t, sandboxTools()...)
	got, _ := Bridge(context.Background(), "sb", srv)
	ctx := tools.WithToolCallContext(context.Background(), "sess", "tc1", t.TempDir(), 2048)

	res, err := got[0].Execute(ctx, json.RawMessage(`{"container_id":"abc"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.HasPrefix(res.Preview, "sandbox_exec:") {
		t.Fatalf("preview = %q, want routed through the raw tool name", res.Preview)
	}
	if !strings.Contains(res.Preview, "abc") {
		t.Fatalf("args not forwarded: %q", res.Preview)
	}
}

func TestBridgedTool_Execute_MarksResultTrusted(t *testing.T) {
	srv, _ := newInMemoryMounted(t, sandboxTools()...)
	got, _ := Bridge(context.Background(), "sb", srv)
	ctx := tools.WithToolCallContext(context.Background(), "sess", "tc1", t.TempDir(), 2048)

	res, err := got[0].Execute(ctx, json.RawMessage(`{"container_id":"abc"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Provenance == nil {
		t.Fatal("bridged MCP result must carry provenance")
	}
	if res.Provenance.Trust != tools.TrustTrusted {
		t.Fatalf("trust = %q, want trusted", res.Provenance.Trust)
	}
	if res.Provenance.Source != "mcp:sb__sandbox_exec" {
		t.Fatalf("source = %q, want mcp:sb__sandbox_exec", res.Provenance.Source)
	}
}

// TestBridgedTool_RoutesRawName guards the wire-name/model-name separation: even
// though the model-facing spec.Name is namespaced, Execute must route CallToolText
// by the RAW server-side tool name.
func TestBridgedTool_RoutesRawName(t *testing.T) {
	srv, _ := newInMemoryMounted(t, sandboxTools()...)
	got, _ := Bridge(context.Background(), "sb", srv)
	if got[0].Spec().Name != "sb__sandbox_exec" {
		t.Fatalf("precondition: model name not namespaced, got %q", got[0].Spec().Name)
	}
	ctx := tools.WithToolCallContext(context.Background(), "sess", "tc1", t.TempDir(), 2048)
	res, err := got[0].Execute(ctx, json.RawMessage(`{"container_id":"abc"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.HasPrefix(res.Preview, "sandbox_exec:") {
		t.Fatalf("Execute routed by %q, want the RAW wire name sandbox_exec", res.Preview)
	}
}

func TestBridgedTool_Execute_PropagatesError(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
	server.AddTool(sandboxTools()[0], func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		return nil, errors.New("boom")
	})
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	srv := NewMountedServer("fixture", nil)
	session, err := connectClient(ctx, clientTransport, mcpSessionOptionsFor(srv))
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	srv.Attach(session)

	got, _ := Bridge(ctx, "sb", srv)
	callCtx := tools.WithToolCallContext(context.Background(), "sess", "tc1", t.TempDir(), 2048)
	_, execErr := got[0].Execute(callCtx, json.RawMessage(`{"container_id":"abc"}`))
	if execErr == nil || !strings.Contains(execErr.Error(), "boom") {
		t.Fatalf("tool-level failure = %v, want the server's error surfaced", execErr)
	}
}

// TestBridgedTool_Execute_BadArgsIsGoError covers Execute's args-unmarshal branch:
// malformed JSON for the tool arguments is a contract violation the model can't
// self-correct from, so it propagates as a Go error (NOT inline error content),
// and the underlying server is never called.
func TestBridgedTool_Execute_BadArgsIsGoError(t *testing.T) {
	srv, _ := newInMemoryMounted(t, sandboxTools()...)
	got, _ := Bridge(context.Background(), "sb", srv)
	ctx := tools.WithToolCallContext(context.Background(), "sess", "tc1", t.TempDir(), 2048)

	_, err := got[0].Execute(ctx, json.RawMessage(`{not valid json`))
	if err == nil {
		t.Fatal("malformed args JSON must propagate as a Go error")
	}
	if !strings.Contains(err.Error(), "sandbox_exec args") {
		t.Fatalf("error should name the tool whose args failed, got %v", err)
	}
}

func TestMount_Namespaced(t *testing.T) {
	reg := tools.NewRegistry()
	srv, _ := newInMemoryMounted(t, sandboxTools()...)

	names, err := Mount(context.Background(), reg, "sb", srv)
	if err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("want 2 registered, got %v", names)
	}
	if _, ok := reg.Get("sb__sandbox_exec"); !ok {
		t.Fatal("sb__sandbox_exec not registered")
	}
	if _, ok := reg.Get("sandbox_exec"); ok {
		t.Fatal("raw sandbox_exec must not be registered — only the namespaced name")
	}
}

func TestMount_RefusesDuplicateWithinServer(t *testing.T) {
	reg := tools.NewRegistry()
	// A server whose two advertised names collide raw (both sanitize/namespace to
	// the same "srv__dup" and share the SAME raw wire name "dup") must be refused
	// all-or-nothing. AddTool on the SDK server keyed by name cannot itself carry
	// two entries named "dup" (the second AddTool call would just replace the
	// first), so this drives the duplicate-raw-name refusal by asserting
	// registerBridged directly against two bridgedTool values sharing a raw name —
	// the exact invariant Mount's caller depends on.
	srv, _ := newInMemoryMounted(t, mustTool("dup", "a", nil, nil))
	bridged, err := Bridge(context.Background(), "srv", srv)
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	dupBridged := append(bridged, bridged[0])
	if _, err := registerBridged(reg, dupBridged); err == nil {
		t.Fatal("want duplicate-name error, got nil")
	}
	if _, ok := reg.Get("srv__dup"); ok {
		t.Fatal("nothing should be registered when the batch has a duplicate")
	}
}

// errListTools is the scripted tools/list failure used to drive Mount error paths
// for a server that boots but cannot enumerate tools — driven by closing the
// server session before the client lists, a genuine transport failure over the
// real wire rather than a scripted Go error.
func TestMount_ListToolsErrorPropagates(t *testing.T) {
	reg := tools.NewRegistry()
	srv, _ := newInMemoryMounted(t, sandboxTools()...)
	// Kill the CLIENT half directly (mirrors bridge_supervisor_test.go's
	// killLiveSession): closing the server half from inside the test body via
	// Server.Sessions() deadlocks against the subscriptions/listen in-flight
	// call ToolListChanged registration keeps open (ServerSession.Close waits
	// for in-flight handlers to unwind).
	killLiveSession(t, srv)

	names, err := Mount(context.Background(), reg, "sb", srv)
	if err == nil {
		t.Fatal("Mount should propagate the tools/list failure after the peer closed")
	}
	if names != nil {
		t.Fatalf("names must be nil on list failure, got %v", names)
	}
	if len(reg.All()) != 0 {
		t.Fatalf("registry must stay empty on list failure, got %d tools", len(reg.All()))
	}
}

// TestFirstLine covers firstLine's branches: a normal multi-line description
// yields the first non-empty trimmed line, leading blank lines are skipped, and
// an all-whitespace/empty input yields "" (the no-summary fallback).
func TestFirstLine(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"first line", "Send an email. \nDetailed help.", "Send an email."},
		{"skips leading blanks", "\n  \nReal summary\nmore", "Real summary"},
		{"empty string", "", ""},
		{"only whitespace", "  \n\t\n  ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstLine(tc.in); got != tc.want {
				t.Fatalf("firstLine(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestBridge_MountsAllAdvertisedTools(t *testing.T) {
	srv, _ := newInMemoryMounted(t,
		mustTool("read_doc", "Read a document.", nil, nil),
		mustTool("delete_doc", "Delete a document.", nil, nil),
	)
	bridged, err := Bridge(context.Background(), "docs", srv)
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	names := map[string]struct{}{}
	for _, b := range bridged {
		names[b.Spec().Name] = struct{}{}
	}
	for _, want := range []string{"docs__read_doc", "docs__delete_doc"} {
		if _, ok := names[want]; !ok {
			t.Fatalf("bridged tools = %v, missing %s", names, want)
		}
	}
}

// TestMount_CollisionHash exercises residual collision disambiguation: two
// distinct raw tool names that sanitize to the SAME namespaced name must not be
// dropped — the second gets a deterministic hash suffix so both register.
func TestMount_CollisionHash(t *testing.T) {
	reg := tools.NewRegistry()
	// "a.b" and "a/b" both sanitize to "a_b" -> namespaced "srv__a_b" collides.
	srv, _ := newInMemoryMounted(t,
		mustTool("a.b", "first", nil, nil),
		mustTool("a/b", "second", nil, nil),
	)
	names, err := Mount(context.Background(), reg, "srv", srv)
	if err != nil {
		t.Fatalf("Mount with sanitize-collision must disambiguate, got %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("both tools must register after disambiguation, got %v", names)
	}
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
	if names[0] == names[1] {
		t.Fatalf("disambiguation failed: both names equal %q", names[0])
	}
	for _, n := range names {
		if _, ok := reg.Get(n); !ok {
			t.Errorf("registered name %q not retrievable", n)
		}
	}
}
