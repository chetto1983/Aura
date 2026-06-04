package mcptools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
)

// httpMCPServer stands up an httptest streamable-HTTP MCP server that completes the
// initialize handshake and serves a scripted tools/list, so the managed/HTTP mount
// helpers run end-to-end through the real *mcp.HTTPClient transport — no subprocess,
// no Docker. The returned closer is goleak-relevant: every mount that opens it must
// be Close()d or TestMain trips on a leaked persistConn.
func httpMCPServer(t *testing.T, defs []mcp.ToolDef) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			// Session teardown on Close(): accept it so Close returns nil cleanly.
			w.WriteHeader(http.StatusOK)
			return
		}
		var req struct {
			ID     *int64 `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "sess-managed")
			writeManagedRPC(t, w, req.ID, map[string]any{"protocolVersion": "2025-06-18"})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			writeManagedRPC(t, w, req.ID, map[string]any{"tools": defs})
		default:
			t.Errorf("unexpected method %q", req.Method)
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeManagedRPC(t *testing.T, w http.ResponseWriter, id *int64, result any) {
	t.Helper()
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  json.RawMessage(raw),
	}); err != nil {
		t.Fatalf("encode rpc: %v", err)
	}
}

func managedDefs() []mcp.ToolDef {
	return []mcp.ToolDef{
		{Name: "read_doc", Description: "Read a document.", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "delete_doc", Description: "Delete a document.", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
}

// TestMountManagedServerWithPolicy_HTTPSuccess drives MountManagedServerWithPolicy
// and openManagedServer through the real streamable-HTTP transport: a managed server
// with a URL opens via OpenServer, its policy-allowed tool is mounted, and the
// destructive tool is returned as a blocked PolicyDecision. The closer must shut the
// HTTP session down cleanly (goleak guard in TestMain).
func TestMountManagedServerWithPolicy_HTTPSuccess(t *testing.T) {
	httpSrv := httpMCPServer(t, managedDefs())
	reg := tools.NewRegistry()
	server := mcp.ManagedServer{
		Type: mcp.ServerTypeStreamableHTTP,
		URL:  httpSrv.URL,
		ToolPolicy: mcp.ManagedToolPolicy{
			Allow:    []string{"read_doc", "delete_doc"},
			DenyRisk: []string{mcpmanager.RiskDestructive},
		},
	}

	closer, names, blocked, err := MountManagedServerWithPolicy(context.Background(), reg, "docs", server)
	if err != nil {
		t.Fatalf("MountManagedServerWithPolicy: %v", err)
	}
	if closer == nil {
		t.Fatal("success must return a non-nil closer")
	}
	defer func() {
		if cerr := closer(); cerr != nil {
			t.Errorf("closer: %v", cerr)
		}
	}()

	if len(names) != 1 || names[0] != "docs__read_doc" {
		t.Fatalf("only read_doc should mount, got %v", names)
	}
	if _, ok := reg.Get("docs__read_doc"); !ok {
		t.Fatal("docs__read_doc not registered")
	}
	if _, ok := reg.Get("docs__delete_doc"); ok {
		t.Fatal("delete_doc is destructive and must be blocked")
	}
	if len(blocked) != 1 || blocked[0].ToolName != "delete_doc" || blocked[0].Allowed {
		t.Fatalf("delete_doc must be the single blocked decision, got %#v", blocked)
	}
}

// TestMountManagedServerWithPolicy_OpenFailure covers openManagedServer's error
// return for the stdio branch: a blocked-trust server makes RuntimeLaunchConfig fail
// before any process spawns, so the helper returns the error with a nil closer and an
// untouched registry.
func TestMountManagedServerWithPolicy_OpenFailure(t *testing.T) {
	reg := tools.NewRegistry()
	// No type, no URL → stdio branch → RuntimeLaunchConfig. With no trust class and
	// no recipe source the server resolves to TrustBlocked, so launch config errors
	// out before spawning anything.
	server := mcp.ManagedServer{Command: "anything"}

	closer, names, blocked, err := MountManagedServerWithPolicy(context.Background(), reg, "blocked", server)
	if err == nil {
		t.Fatal("a blocked-trust server must fail to open")
	}
	if closer != nil {
		t.Fatal("on open failure closer must be nil")
	}
	if names != nil || blocked != nil {
		t.Fatalf("names/blocked must be nil on open failure, got names=%v blocked=%v", names, blocked)
	}
	if len(reg.All()) != 0 {
		t.Fatalf("registry must stay empty on open failure, got %d tools", len(reg.All()))
	}
}

// TestMountManagedServerWithPolicy_MountFailureReapsServer covers the
// MountManagedServerWithPolicy failure path AFTER a successful open: the transport
// opens and lists tools, but registration collides with an already-registered name.
// The helper must wrap the error, return nil names/blocked, and Close the opened
// transport (no leaked HTTP session — TestMain enforces this).
func TestMountManagedServerWithPolicy_MountFailureReapsServer(t *testing.T) {
	httpSrv := httpMCPServer(t, []mcp.ToolDef{
		{Name: "read_doc", Description: "Read a document.", InputSchema: json.RawMessage(`{"type":"object"}`)},
	})
	reg := tools.NewRegistry()
	// Pre-register the namespaced name so the policy-allowed tool collides on mount.
	if _, err := Mount(context.Background(), reg, "docs",
		&fakeServer{defs: []mcp.ToolDef{{Name: "read_doc", Description: "Read a document."}}}, nil); err != nil {
		t.Fatalf("seed Mount: %v", err)
	}

	server := mcp.ManagedServer{
		Type:       mcp.ServerTypeStreamableHTTP,
		URL:        httpSrv.URL,
		ToolPolicy: mcp.ManagedToolPolicy{Allow: []string{"read_doc"}},
	}
	closer, names, blocked, err := MountManagedServerWithPolicy(context.Background(), reg, "docs", server)
	if err == nil || !strings.Contains(err.Error(), "collision") {
		t.Fatalf("want a wrapped registration collision error, got %v", err)
	}
	if closer != nil {
		t.Fatal("on mount failure closer must be nil (the transport is reaped internally)")
	}
	if names != nil || blocked != nil {
		t.Fatalf("names/blocked must be nil on mount failure, got names=%v blocked=%v", names, blocked)
	}
}

// TestMountServerWithPolicy_SpawnFailureLeavesRegistryClean covers
// MountServerWithPolicy's open-failure early return: a missing binary makes mcp.Open
// fail, so the helper returns the error with nil closer/names/blocked and an
// untouched registry — the policy variant of the plain MountServer spawn-failure
// guard.
func TestMountServerWithPolicy_SpawnFailureLeavesRegistryClean(t *testing.T) {
	reg := tools.NewRegistry()
	closer, names, blocked, err := MountServerWithPolicy(context.Background(), reg, "bad",
		mcp.ServerConfig{Command: "aura-nonexistent-mcp-binary-xyz"}, mcp.ManagedServer{})
	if err == nil {
		t.Fatal("want spawn error for a missing binary")
	}
	if closer != nil {
		t.Fatal("on failure closer must be nil")
	}
	if names != nil || blocked != nil {
		t.Fatalf("names/blocked must be nil on spawn failure, got names=%v blocked=%v", names, blocked)
	}
	if len(reg.All()) != 0 {
		t.Fatalf("registry must stay empty on spawn failure, got %d tools", len(reg.All()))
	}
}

// TestOpenManagedServer_StdioBranchFailure covers openManagedServer's stdio branch
// (no Type, no URL) directly: with a non-blocked trust class RuntimeLaunchConfig
// succeeds, then mcp.Open fails on the missing binary, so the error propagates with
// no transport returned.
func TestOpenManagedServer_StdioBranchFailure(t *testing.T) {
	server := mcp.ManagedServer{
		Command: "aura-nonexistent-mcp-binary-xyz",
		Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedLocal},
	}
	_, err := openManagedServer(context.Background(), "stdio", server)
	if err == nil {
		t.Fatal("want spawn error from mcp.Open on a missing binary")
	}
	// NB: the returned transport is NOT asserted nil here. mcp.Open returns a
	// concrete *mcp.Client; openManagedServer's `return mcp.Open(...)` widens that
	// nil pointer into a non-nil mcp.Transport interface (the classic typed-nil
	// quirk). Every caller checks err first and never touches the transport on
	// error, so this is harmless — but it means the interface value is non-nil.
}

// TestOpenManagedServer_HTTPBranch covers openManagedServer's HTTP branch via Type
// and via a bare URL (Type empty), confirming both routes reach OpenServer and
// return a usable transport that lists the scripted tools.
func TestOpenManagedServer_HTTPBranch(t *testing.T) {
	httpSrv := httpMCPServer(t, managedDefs())
	cases := []struct {
		name   string
		server mcp.ManagedServer
	}{
		{"explicit type", mcp.ManagedServer{Type: mcp.ServerTypeStreamableHTTP, URL: httpSrv.URL}},
		{"bare url infers http", mcp.ManagedServer{URL: httpSrv.URL}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			transport, err := openManagedServer(context.Background(), "docs", tc.server)
			if err != nil {
				t.Fatalf("openManagedServer: %v", err)
			}
			defer func() {
				if cerr := transport.Close(); cerr != nil {
					t.Errorf("close: %v", cerr)
				}
			}()
			defs, err := transport.ListTools(context.Background())
			if err != nil {
				t.Fatalf("ListTools: %v", err)
			}
			if len(defs) != 2 {
				t.Fatalf("want 2 tools from the fixture, got %d", len(defs))
			}
		})
	}
}
