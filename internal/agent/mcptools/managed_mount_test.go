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
)

// httpMCPServer stands up an httptest streamable-HTTP MCP server that completes
// the initialize handshake and serves a scripted tools/list, so the managed/HTTP
// mount helpers run end-to-end through the real *mcp.HTTPClient transport.
func httpMCPServer(t *testing.T, defs []mcp.ToolDef) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
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

// TestMountManagedServer_HTTPSuccess drives MountManagedServer through the real
// streamable-HTTP transport. Every advertised tool mounts and the closer shuts the
// HTTP session down cleanly.
func TestMountManagedServer_HTTPSuccess(t *testing.T) {
	httpSrv := httpMCPServer(t, managedDefs())
	reg := tools.NewRegistry()
	server := mcp.ManagedServer{
		Type: mcp.ServerTypeStreamableHTTP,
		URL:  httpSrv.URL,
	}

	closer, names, err := MountManagedServer(context.Background(), reg, "docs", server)
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

// TestMountManagedServer_OpenFailure covers MountManagedServer's error return for
// the stdio branch: a blocked-trust server makes RuntimeLaunchConfig fail before
// any process spawns.
func TestMountManagedServer_OpenFailure(t *testing.T) {
	reg := tools.NewRegistry()
	server := mcp.ManagedServer{Command: "anything"}

	closer, names, err := MountManagedServer(context.Background(), reg, "blocked", server)
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
// successful open: the transport opens and lists tools, but registration collides
// with an already-registered name. The helper closes the opened transport.
func TestMountManagedServer_MountFailureReapsServer(t *testing.T) {
	httpSrv := httpMCPServer(t, []mcp.ToolDef{
		{Name: "read_doc", Description: "Read a document.", InputSchema: json.RawMessage(`{"type":"object"}`)},
	})
	reg := tools.NewRegistry()
	if _, err := Mount(context.Background(), reg, "docs",
		&fakeServer{defs: []mcp.ToolDef{{Name: "read_doc", Description: "Read a document."}}}); err != nil {
		t.Fatalf("seed Mount: %v", err)
	}

	server := mcp.ManagedServer{
		Type: mcp.ServerTypeStreamableHTTP,
		URL:  httpSrv.URL,
	}
	closer, names, err := MountManagedServer(context.Background(), reg, "docs", server)
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

// TestMountManagedServer_HTTPBranchInfersFromBareURL covers the HTTP branch via a
// bare URL (no explicit Type): MountManagedServer must infer streamable-HTTP and
// mount every advertised tool, exercising the same branch the deleted dead
// openManagedServer helper used to (AG-028 — the helper was unreachable production
// code; the live MountManagedServer inlines the identical branch).
func TestMountManagedServer_HTTPBranchInfersFromBareURL(t *testing.T) {
	httpSrv := httpMCPServer(t, managedDefs())
	reg := tools.NewRegistry()
	server := mcp.ManagedServer{URL: httpSrv.URL} // bare URL, no Type → inferred HTTP

	closer, names, err := MountManagedServer(context.Background(), reg, "docs", server)
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
// branch: a non-blocked trust class makes RuntimeLaunchConfig succeed, then mcp.Open
// fails on the missing binary (the stdio branch the deleted openManagedServer helper
// duplicated — AG-028).
func TestMountManagedServer_StdioBranchFailure(t *testing.T) {
	reg := tools.NewRegistry()
	server := mcp.ManagedServer{
		Command: "aura-nonexistent-mcp-binary-xyz",
		Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedLocal},
	}
	closer, names, err := MountManagedServer(context.Background(), reg, "stdio", server)
	if err == nil {
		t.Fatal("want spawn error from mcp.Open on a missing binary")
	}
	if closer != nil {
		t.Fatal("on stdio open failure closer must be nil")
	}
	if names != nil {
		t.Fatalf("on stdio open failure names must be nil, got %v", names)
	}
}
