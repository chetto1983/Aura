package mcptools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
)

// fakeServer is an in-memory Server: it records the last CallTool and returns
// scripted ListTools/CallTool results, so the bridge is tested without a process.
type fakeServer struct {
	defs         []mcp.ToolDef
	listErr      error
	callText     string
	callErr      error
	lastName     string
	lastArgs     map[string]any
	CallToolFunc func(ctx context.Context, name string, args map[string]any) (string, error)
}

func (f *fakeServer) ListTools(context.Context) ([]mcp.ToolDef, error) { return f.defs, f.listErr }
func (f *fakeServer) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	f.lastName, f.lastArgs = name, args
	if f.CallToolFunc != nil {
		return f.CallToolFunc(ctx, name, args)
	}
	return f.callText, f.callErr
}

type fakeReconnectClient struct {
	defs      []mcp.ToolDef
	callText  string
	callErr   error
	callCount int
	closed    bool
}

func (f *fakeReconnectClient) ListTools(context.Context) ([]mcp.ToolDef, error) {
	return f.defs, nil
}

func (f *fakeReconnectClient) CallTool(context.Context, string, map[string]any) (string, error) {
	f.callCount++
	return f.callText, f.callErr
}

func (f *fakeReconnectClient) Close() error {
	f.closed = true
	return nil
}

// Ping is unused by the reconnect tests in this file (they drive reconnect via
// ListTools/CallTool transport errors); returning nil keeps the double
// interface-complete. bridge_ping_test.go has dedicated ping fakes.
func (f *fakeReconnectClient) Ping(context.Context) error { return nil }

type blockingReconnectClient struct {
	defs    []mcp.ToolDef
	started chan struct{}
	release chan struct{}
	closed  chan struct{}
}

func (f *blockingReconnectClient) ListTools(context.Context) ([]mcp.ToolDef, error) {
	return f.defs, nil
}

func (f *blockingReconnectClient) CallTool(ctx context.Context, _ string, _ map[string]any) (string, error) {
	close(f.started)
	select {
	case <-f.release:
		return "released", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (f *blockingReconnectClient) Close() error {
	close(f.closed)
	return nil
}

// Ping is unused by TestReconnectServerCloseDoesNotWaitForBlockedCall (it blocks
// CallTool, not Ping); returning nil keeps the double interface-complete.
func (f *blockingReconnectClient) Ping(context.Context) error { return nil }

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
	got, err := Bridge(context.Background(), "sb", &fakeServer{defs: sandboxDefs()})
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
	// The deferred Summary is manifest-visible, so MCP text must be framed as
	// untrusted while keeping the required-args hint visible for call shape.
	if !strings.Contains(strings.ToLower(exec.Summary), "untrusted mcp server") ||
		!strings.Contains(strings.ToLower(exec.Summary), "data") ||
		!strings.Contains(exec.Summary, "Execute commands in the sandboxed environment.") ||
		!strings.Contains(exec.Summary, "Required args: container_id.") {
		t.Fatalf("summary should be framed and include required-args hint, got %q", exec.Summary)
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

func TestBridge_MemoryNamespaceToolsAreDeferredByDefault(t *testing.T) {
	got, err := Bridge(context.Background(), "memory", &fakeServer{defs: []mcp.ToolDef{
		{Name: "memory_search", Description: "Search memory.", Annotations: mcp.ToolAnnotations{ReadOnlyHint: true}},
		{Name: "memory_add_fact", Description: "Store a durable fact."},
	}})
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 memory tools, got %d", len(got))
	}
	// The memory namespace lost its non-deferred exception: six full schemas in every
	// request bought ~2.7k tokens to answer "ok", for a capability the model reaches via
	// tool_search exactly like the 14 calendar and 14 whatsapp tools already did.
	for _, tool := range got {
		if !tool.Spec().Deferred {
			t.Fatalf("%s Deferred = false; every bridged tool is deferred", tool.Spec().Name)
		}
	}
}

func TestReconnectServerRetriesAndRefreshesToolSearch(t *testing.T) {
	oldOpen := openMCPClient
	t.Cleanup(func() { openMCPClient = oldOpen })

	initial := &fakeReconnectClient{
		defs:    []mcp.ToolDef{{Name: "send", Description: "Old delivery path."}},
		callErr: fmt.Errorf("pipe closed: %w", mcp.ErrTransport),
	}
	refreshed := &fakeReconnectClient{
		defs:     []mcp.ToolDef{{Name: "send", Description: "Fresh delivery path.", Annotations: mcp.ToolAnnotations{ReadOnlyHint: true}}},
		callText: "sent",
	}
	opens := 0
	openMCPClient = func(context.Context, context.Context, string, mcp.ServerConfig) (reconnectingClient, error) {
		opens++
		return refreshed, nil
	}

	reg := tools.NewRegistry()
	ts := &tools.ToolSearch{Registry: reg}
	reg.Register(ts)
	srv := newReconnectingServer("mail", mcp.ServerConfig{Command: "mail-mcp"}, initial)
	names, err := Mount(context.Background(), reg, "mail", srv)
	if err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if len(names) != 1 {
		t.Fatalf("mounted names = %v", names)
	}
	searchCtx := tools.WithToolCallContext(context.Background(), "sess", "search-1", t.TempDir(), 2048)
	if res, err := ts.Execute(searchCtx, json.RawMessage(`{"query":"old"}`)); err != nil || !strings.Contains(res.Preview, "Old delivery path.") {
		t.Fatalf("pre-reconnect search = (%q,%v), want old description", res.Preview, err)
	}

	tool, _ := reg.Get(names[0])
	callCtx := tools.WithToolCallContext(context.Background(), "sess", "call-1", t.TempDir(), 2048)
	_, err = tool.Execute(callCtx, json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "not replayed") {
		t.Fatalf("Execute after reconnect = %v, want no-replay error", err)
	}
	if opens != 1 || !initial.closed {
		t.Fatalf("reconnect did not close/reopen exactly once: opens=%d closed=%v", opens, initial.closed)
	}
	if initial.callCount != 1 || refreshed.callCount != 0 {
		t.Fatalf("call replay policy mismatch: initial=%d refreshed=%d", initial.callCount, refreshed.callCount)
	}
	if got := tool.Spec().Description; !strings.Contains(got, "Fresh delivery path.") || !strings.Contains(got, "untrusted MCP server description") {
		t.Fatalf("refreshed description = %q", got)
	}
	if got := tool.Spec().Summary; !strings.Contains(got, "Fresh delivery path.") || !strings.Contains(got, "untrusted MCP server summary data") {
		t.Fatalf("refreshed summary = %q", got)
	}
	if tool.Spec().Mutating {
		t.Fatal("refreshed readOnlyHint=true should set Mutating:false")
	}
	searchCtx2 := tools.WithToolCallContext(context.Background(), "sess", "search-2", t.TempDir(), 2048)
	if res, err := ts.Execute(searchCtx2, json.RawMessage(`{"query":"fresh"}`)); err != nil || !strings.Contains(res.Preview, "Fresh delivery path.") {
		t.Fatalf("post-reconnect search = (%q,%v), want refreshed description", res.Preview, err)
	}
}

func TestReconnectServerSecondTransportFailurePropagates(t *testing.T) {
	oldOpen := openMCPClient
	t.Cleanup(func() { openMCPClient = oldOpen })

	initial := &fakeReconnectClient{
		defs:    []mcp.ToolDef{{Name: "send", Description: "Send."}},
		callErr: fmt.Errorf("first fail: %w", mcp.ErrTransport),
	}
	reopened := &fakeReconnectClient{
		defs:    []mcp.ToolDef{{Name: "send", Description: "Send."}},
		callErr: fmt.Errorf("second fail: %w", mcp.ErrTransport),
	}
	openMCPClient = func(context.Context, context.Context, string, mcp.ServerConfig) (reconnectingClient, error) {
		return reopened, nil
	}

	srv := newReconnectingServer("mail", mcp.ServerConfig{Command: "mail-mcp"}, initial)
	bridged, err := Bridge(context.Background(), "mail", srv)
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	ctx := tools.WithToolCallContext(context.Background(), "sess", "call-1", t.TempDir(), 2048)
	_, err = bridged[0].Execute(ctx, json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), "not replayed") {
		t.Fatalf("second transport failure = %v, want propagated no-replay error", err)
	}
	if initial.callCount != 1 || reopened.callCount != 0 {
		t.Fatalf("call replay policy mismatch: initial=%d reopened=%d", initial.callCount, reopened.callCount)
	}
}

func TestReconnectServerCloseDoesNotWaitForBlockedCall(t *testing.T) {
	client := &blockingReconnectClient{
		defs:    []mcp.ToolDef{{Name: "send", Description: "Send."}},
		started: make(chan struct{}),
		release: make(chan struct{}),
		closed:  make(chan struct{}),
	}
	srv := newReconnectingServer("mail", mcp.ServerConfig{Command: "mail-mcp"}, client)
	callDone := make(chan struct{})
	go func() {
		_, _ = srv.CallTool(context.Background(), "send", nil)
		close(callDone)
	}()

	select {
	case <-client.started:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("blocked call did not start")
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- srv.Close() }()
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Close waited behind an in-flight MCP call")
	}
	select {
	case <-client.closed:
	default:
		t.Fatal("Close did not close the underlying client")
	}

	close(client.release)
	select {
	case <-callDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("blocked call did not finish after release")
	}
}

func TestBridgedToolExecuteAppliesConfiguredCallTimeout(t *testing.T) {
	t.Setenv(envMCPCallTimeoutSec, "0.025")
	srv := &fakeServer{defs: sandboxDefs()}
	srv.callErr = nil
	srv.callText = ""
	srvCallStarted := make(chan struct{})
	srvCallDone := make(chan struct{})
	srv.CallToolFunc = func(ctx context.Context, name string, args map[string]any) (string, error) {
		close(srvCallStarted)
		defer close(srvCallDone)
		<-ctx.Done()
		return "", ctx.Err()
	}
	got, _ := Bridge(context.Background(), "sb", srv)
	ctx := tools.WithToolCallContext(context.Background(), "sess", "tc1", t.TempDir(), 2048)

	start := time.Now()
	_, err := got[0].Execute(ctx, json.RawMessage(`{"container_id":"abc"}`))
	elapsed := time.Since(start)
	if err == nil || !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("Execute error = %v, want propagated deadline exceeded", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("configured call timeout was not applied; elapsed=%v", elapsed)
	}
	select {
	case <-srvCallStarted:
	default:
		t.Fatal("server CallTool was not reached")
	}
	select {
	case <-srvCallDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("server CallTool did not observe context cancellation")
	}
}

func TestBridgedTool_Execute_RoutesAndWraps(t *testing.T) {
	srv := &fakeServer{defs: sandboxDefs(), callText: "42"}
	got, _ := Bridge(context.Background(), "sb", srv)
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

func TestBridgedTool_Execute_MarksResultUntrusted(t *testing.T) {
	srv := &fakeServer{defs: sandboxDefs(), callText: "external payload"}
	got, _ := Bridge(context.Background(), "sb", srv)
	ctx := tools.WithToolCallContext(context.Background(), "sess", "tc1", t.TempDir(), 2048)

	res, err := got[0].Execute(ctx, json.RawMessage(`{"container_id":"abc"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Provenance == nil {
		t.Fatal("bridged MCP result must carry provenance")
	}
	if res.Provenance.Trust != tools.TrustUntrusted {
		t.Fatalf("trust = %q, want untrusted", res.Provenance.Trust)
	}
	if res.Provenance.Source != "mcp:sb__sandbox_exec" {
		t.Fatalf("source = %q, want mcp:sb__sandbox_exec", res.Provenance.Source)
	}
}

// TestBridgedTool_RoutesRawName guards the wire-name/model-name separation: even
// though the model-facing spec.Name is namespaced, Execute must route CallTool by
// the RAW server-side tool name. Kills the "route by spec.Name" mutant.
func TestBridgedTool_RoutesRawName(t *testing.T) {
	srv := &fakeServer{defs: sandboxDefs(), callText: "ok"}
	got, _ := Bridge(context.Background(), "sb", srv)
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

func TestBridgedTool_Execute_PropagatesError(t *testing.T) {
	srv := &fakeServer{defs: sandboxDefs(), callErr: context.DeadlineExceeded}
	got, _ := Bridge(context.Background(), "sb", srv)
	ctx := tools.WithToolCallContext(context.Background(), "sess", "tc1", t.TempDir(), 2048)

	_, err := got[0].Execute(ctx, json.RawMessage(`{"container_id":"abc"}`))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("tool-level failure = %v, want propagated deadline", err)
	}
}

// TestBridgedTool_Execute_BadArgsIsGoError covers Execute's args-unmarshal branch:
// malformed JSON for the tool arguments is a contract violation the model can't
// self-correct from, so it propagates as a Go error (NOT inline error content), and
// the underlying server is never called.
func TestBridgedTool_Execute_BadArgsIsGoError(t *testing.T) {
	srv := &fakeServer{defs: sandboxDefs(), callText: "unused"}
	got, _ := Bridge(context.Background(), "sb", srv)
	ctx := tools.WithToolCallContext(context.Background(), "sess", "tc1", t.TempDir(), 2048)

	_, err := got[0].Execute(ctx, json.RawMessage(`{not valid json`))
	if err == nil {
		t.Fatal("malformed args JSON must propagate as a Go error")
	}
	if !strings.Contains(err.Error(), "sandbox_exec args") {
		t.Fatalf("error should name the tool whose args failed, got %v", err)
	}
	if srv.lastName != "" {
		t.Fatalf("the server must not be called when args fail to parse, got call to %q", srv.lastName)
	}
}

func TestMount_Namespaced(t *testing.T) {
	reg := tools.NewRegistry()
	srv := &fakeServer{defs: sandboxDefs()}

	names, err := Mount(context.Background(), reg, "sb", srv)
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
	if _, err := Mount(context.Background(), reg, "sb", srv); err == nil {
		t.Fatal("want collision error on re-mount, got nil")
	}
}

func TestMount_RefusesDuplicateWithinServer(t *testing.T) {
	reg := tools.NewRegistry()
	srv := &fakeServer{defs: []mcp.ToolDef{
		{Name: "dup", Description: "a"},
		{Name: "dup", Description: "b"},
	}}
	if _, err := Mount(context.Background(), reg, "srv", srv); err == nil {
		t.Fatal("want duplicate-name error, got nil")
	}
	if _, ok := reg.Get("srv__dup"); ok {
		t.Fatal("nothing should be registered when the batch has a duplicate")
	}
}

// errListTools is the scripted tools/list failure used to drive Mount error paths
// for a server that boots but cannot enumerate tools.
var errListTools = errors.New("tools/list unavailable")

// TestMount_ListToolsErrorPropagates covers Mount's early-return: when Bridge's
// tools/list fails, Mount returns that error verbatim, registers nothing, and hands
// back nil names — the registry is never half-wired.
func TestMount_ListToolsErrorPropagates(t *testing.T) {
	reg := tools.NewRegistry()
	srv := &fakeServer{listErr: errListTools}

	names, err := Mount(context.Background(), reg, "sb", srv)
	if !errors.Is(err, errListTools) {
		t.Fatalf("Mount should propagate the tools/list error, got %v", err)
	}
	if names != nil {
		t.Fatalf("names must be nil on list failure, got %v", names)
	}
	if len(reg.All()) != 0 {
		t.Fatalf("registry must stay empty on list failure, got %d tools", len(reg.All()))
	}
}

// TestFirstLine covers firstLine's branches: a normal multi-line description yields
// the first non-empty trimmed line, leading blank lines are skipped, and an
// all-whitespace/empty input yields "" (the no-summary fallback). The empty-return
// branch is otherwise unexercised because real MCP descriptions are non-empty.
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

// TestBridge_ListToolsErrorPropagates covers Bridge's early error return directly
// when tools/list fails.
func TestBridge_ListToolsErrorPropagates(t *testing.T) {
	srv := &fakeServer{listErr: errListTools}
	tools, err := Bridge(context.Background(), "sb", srv)
	if !errors.Is(err, errListTools) {
		t.Fatalf("Bridge should propagate the tools/list error, got %v", err)
	}
	if tools != nil {
		t.Fatalf("tools must be nil on list failure, got tools=%v", tools)
	}
}

func TestBridge_MountsAllAdvertisedTools(t *testing.T) {
	srv := &fakeServer{defs: []mcp.ToolDef{
		{Name: "read_doc", Description: "Read a document."},
		{Name: "delete_doc", Description: "Delete a document."},
	}}
	bridged, err := Bridge(context.Background(), "docs", srv)
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	got := []string{bridged[0].Spec().Name, bridged[1].Spec().Name}
	want := []string{"docs__read_doc", "docs__delete_doc"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("bridged tools = %#v, want %#v", got, want)
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
	names, err := Mount(context.Background(), reg, "srv", srv)
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
