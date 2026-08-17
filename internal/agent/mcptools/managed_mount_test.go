package mcptools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
)

// startSDKHTTPFixture serves a real streamable-HTTP MCP endpoint over httptest,
// registering toolDefs with the trivial handler. Real, not a hand-rolled
// JSON-RPC-over-HTTP fake: MCPC-03 forbids reintroducing hand-rolled framing
// anywhere in the tree, tests included.
func startSDKHTTPFixture(t *testing.T, toolDefs ...*sdkmcp.Tool) (*httptest.Server, *sdkmcp.Server) {
	t.Helper()
	srv := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture-managed", Version: "0.0.1"}, nil)
	for _, tool := range toolDefs {
		srv.AddTool(tool, trivialToolHandler)
	}
	handler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return srv }, nil)
	ts := httptest.NewServer(handler)
	t.Cleanup(func() {
		for session := range srv.Sessions() {
			_ = session.Close()
		}
		ts.Close()
	})
	return ts, srv
}

func managedTools() []*sdkmcp.Tool {
	return []*sdkmcp.Tool{
		mustTool("read_doc", "Read a document.", nil, nil),
		mustTool("delete_doc", "Delete a document.", nil, nil),
	}
}

func TestMountManagedServerReturnsProcessOwnedHostClient(t *testing.T) {
	httpSrv, _ := startSDKHTTPFixture(t, managedTools()...)
	reg := tools.NewRegistry()
	server := mcp.ManagedServer{Type: mcp.ServerTypeStreamableHTTP, URL: httpSrv.URL}

	closer, _, host, err := MountManagedServerWithOptions(
		context.Background(),
		context.Background(),
		reg,
		"docs",
		server,
		MountOptions{Egress: mcp.RuntimeEgressPolicy(false, server)},
	)
	if err != nil {
		t.Fatalf("MountManagedServerWithOptions: %v", err)
	}
	t.Cleanup(func() { _ = closer() })
	if host == nil {
		t.Fatal("successful managed mount returned a nil host client")
	}
	text, err := host.CallToolText(context.Background(), "read_doc", map[string]any{})
	if err != nil {
		t.Fatalf("host CallToolText: %v", err)
	}
	if !strings.HasPrefix(text, "read_doc:") {
		t.Fatalf("host CallToolText = %q, want routed to read_doc", text)
	}
}

// TestMountManagedServer_HTTPSuccess drives MountManagedServer through the real
// streamable-HTTP transport. Every advertised tool mounts and the closer shuts
// the session down cleanly.
func TestMountManagedServer_HTTPSuccess(t *testing.T) {
	httpSrv, _ := startSDKHTTPFixture(t, managedTools()...)
	reg := tools.NewRegistry()
	server := mcp.ManagedServer{Type: mcp.ServerTypeStreamableHTTP, URL: httpSrv.URL}

	closer, names, err := MountManagedServer(context.Background(), context.Background(), reg, "docs", server)
	if err != nil {
		t.Fatalf("MountManagedServer: %v", err)
	}
	if closer == nil {
		t.Fatal("success must return a non-nil closer")
	}
	defer func() {
		if cerr := closer(); cerr != nil {
			t.Errorf("closer: %v", cerr)
		}
	}()

	if len(names) != 2 {
		t.Fatalf("all advertised tools should mount, got %v", names)
	}
	for _, want := range []string{"docs__read_doc", "docs__delete_doc"} {
		if _, ok := reg.Get(want); !ok {
			t.Fatalf("%s not registered", want)
		}
	}
}

// TestMountManagedServer_OpenFailure covers MountManagedServer's error return
// for the stdio branch: a blocked-trust server makes RuntimeLaunchConfig fail
// before any process spawns.
func TestMountManagedServer_OpenFailure(t *testing.T) {
	reg := tools.NewRegistry()
	server := mcp.ManagedServer{Command: "anything"}

	closer, names, err := MountManagedServer(context.Background(), context.Background(), reg, "blocked", server)
	if err == nil {
		t.Fatal("a blocked-trust server must fail to open")
	}
	if closer != nil {
		t.Fatal("on open failure closer must be nil")
	}
	if names != nil {
		t.Fatalf("names must be nil on open failure, got names=%v", names)
	}
	if len(reg.All()) != 0 {
		t.Fatalf("registry must stay empty on open failure, got %d tools", len(reg.All()))
	}
}

// TestMountManagedServer_MountFailureReapsServer covers the failure path after a
// successful open: the transport opens and lists tools, but registration
// collides with an already-registered name. The helper closes the opened
// session.
func TestMountManagedServer_MountFailureReapsServer(t *testing.T) {
	httpSrv, _ := startSDKHTTPFixture(t, mustTool("read_doc", "Read a document.", nil, nil))
	reg := tools.NewRegistry()
	seedSrv, _ := newInMemoryMounted(t, mustTool("read_doc", "Read a document.", nil, nil))
	if _, err := Mount(context.Background(), reg, "docs", seedSrv); err != nil {
		t.Fatalf("seed Mount: %v", err)
	}

	server := mcp.ManagedServer{Type: mcp.ServerTypeStreamableHTTP, URL: httpSrv.URL}
	closer, names, err := MountManagedServer(context.Background(), context.Background(), reg, "docs", server)
	if err == nil || !strings.Contains(err.Error(), "collision") {
		t.Fatalf("want a wrapped registration collision error, got %v", err)
	}
	if closer != nil {
		t.Fatal("on mount failure closer must be nil (the transport is reaped internally)")
	}
	if names != nil {
		t.Fatalf("names must be nil on mount failure, got names=%v", names)
	}
}

// TestMountManagedServer_HTTPBranchInfersFromBareURL covers the HTTP branch via
// a bare URL (no explicit Type): MountManagedServer must infer streamable-HTTP
// and mount every advertised tool.
func TestMountManagedServer_HTTPBranchInfersFromBareURL(t *testing.T) {
	httpSrv, _ := startSDKHTTPFixture(t, managedTools()...)
	reg := tools.NewRegistry()
	server := mcp.ManagedServer{URL: httpSrv.URL} // bare URL, no Type -> inferred HTTP

	closer, names, err := MountManagedServer(context.Background(), context.Background(), reg, "docs", server)
	if err != nil {
		t.Fatalf("MountManagedServer (bare url): %v", err)
	}
	if closer == nil {
		t.Fatal("success must return a non-nil closer")
	}
	defer func() {
		if cerr := closer(); cerr != nil {
			t.Errorf("closer: %v", cerr)
		}
	}()
	if len(names) != 2 {
		t.Fatalf("all advertised tools should mount over an inferred-HTTP bare URL, got %v", names)
	}
}

// TestMountManagedServer_StdioBranchFailure covers MountManagedServer's stdio
// branch: a non-blocked trust class makes RuntimeLaunchConfig succeed, then the
// SDK's CommandTransport.Connect fails on the missing binary.
func TestMountManagedServer_StdioBranchFailure(t *testing.T) {
	reg := tools.NewRegistry()
	server := mcp.ManagedServer{
		Command: "aura-nonexistent-mcp-binary-xyz",
		Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedLocal},
	}
	closer, names, err := MountManagedServer(context.Background(), context.Background(), reg, "stdio", server)
	if err == nil {
		t.Fatal("want spawn error on a missing binary")
	}
	if closer != nil {
		t.Fatal("on stdio open failure closer must be nil")
	}
	if names != nil {
		t.Fatalf("on stdio open failure names must be nil, got %v", names)
	}
}
