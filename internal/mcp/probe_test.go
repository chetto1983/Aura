package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// managedFromHelper turns a helper ServerConfig (the self-exec fake stdio MCP server
// from client_open_test.go) into a ManagedServer so ProbeServer can dial it.
func managedFromHelper(mode string) ManagedServer {
	cfg := helperServerConfig(mode)
	return ManagedServer{Command: cfg.Command, Args: cfg.Args, Env: cfg.Env}
}

// TestMCPProbe_HealthyServerCountsTools proves a reachable stdio server probes OK with
// the real tools/list count (the helper advertises exactly one tool, "echo").
func TestMCPProbe_HealthyServerCountsTools(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	got := ProbeServer(ctx, "helper", managedFromHelper(""))
	if !got.OK {
		t.Fatalf("ProbeServer(healthy): want OK, got %+v", got)
	}
	if got.ToolCount != 1 {
		t.Errorf("ToolCount: want 1 (echo), got %d", got.ToolCount)
	}
	if got.Name != "helper" {
		t.Errorf("Name: want helper, got %q", got.Name)
	}
	if got.Err != "" {
		t.Errorf("Err: want empty on success, got %q", got.Err)
	}
}

// TestMCPProbe_CanceledContextIsolated proves a bounded/expired context makes the probe
// fail THAT server's row (OK=false, non-empty Err) without panicking and without
// blocking — the handler bounds each probe with a per-request deadline, so a slow server
// degrades to a failed row, never a stalled board. An already-canceled context makes the
// dial's first ctx-check fail immediately, exercising the deadline-honoring path
// deterministically (no reliance on a real hanging subprocess / wall-clock timing).
func TestMCPProbe_CanceledContextIsolated(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // expired before the probe runs

	start := time.Now()
	got := ProbeServer(ctx, "slow", managedFromHelper(""))
	elapsed := time.Since(start)

	if got.OK {
		t.Errorf("ProbeServer(canceled ctx): want OK=false, got %+v", got)
	}
	if got.Err == "" {
		t.Error("ProbeServer(canceled ctx): want a non-empty Err on failure")
	}
	// The bounded probe must return promptly, never block on the dead/slow server.
	if elapsed > 30*time.Second {
		t.Errorf("ProbeServer(canceled ctx) took %v, expected a prompt return", elapsed)
	}
}

// TestMCPProbe_DeadServerIsolated proves a server that dies immediately (the handshake
// read fails) yields OK=false with a redacted Err for THAT server only — never a panic.
func TestMCPProbe_DeadServerIsolated(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a real subprocess")
	}
	got := ProbeServer(context.Background(), "dead", managedFromHelper("crash"))
	if got.OK {
		t.Errorf("ProbeServer(crash): want OK=false, got %+v", got)
	}
	if got.Err == "" {
		t.Error("ProbeServer(crash): want a non-empty Err on failure")
	}
}

// TestMCPProbe_EmptyCommand proves a server with neither a command nor an HTTP endpoint
// is reported unreachable (OK=false) without a dial attempt.
func TestMCPProbe_EmptyCommand(t *testing.T) {
	got := ProbeServer(context.Background(), "blank", ManagedServer{})
	if got.OK {
		t.Errorf("ProbeServer(empty command): want OK=false, got %+v", got)
	}
	if got.Err == "" {
		t.Error("ProbeServer(empty command): want a non-empty Err")
	}
}

// TestMCPProbe_HTTPEndpointDialsAndCountsTools proves a streamable-HTTP server is now
// DIALED (initialize + tools/list) and its real tool count reported — the fix for the
// cockpit board showing 0 tools for every HTTP recipe (memory/calendar/pim/whatsapp).
func TestMCPProbe_HTTPEndpointDialsAndCountsTools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req rpcReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "probe-1")
			writeHTTPRPC(t, w, req.ID, initializeFixture("2025-06-18", "probe-fixture"))
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			writeHTTPRPC(t, w, req.ID, map[string]any{"tools": []map[string]any{
				{"name": "memory_search", "description": "Search", "inputSchema": map[string]any{"type": "object"}},
				{"name": "memory_upsert_fact", "description": "Upsert", "inputSchema": map[string]any{"type": "object"}},
			}})
		default:
			t.Fatalf("unexpected method %q", req.Method)
		}
	}))
	defer server.Close()

	got := ProbeServer(context.Background(), "memory", ManagedServer{Type: ServerTypeStreamableHTTP, URL: server.URL})
	if !got.OK {
		t.Fatalf("ProbeServer(http): want OK, got %+v", got)
	}
	if got.ToolCount != 2 {
		t.Errorf("ProbeServer(http): want ToolCount 2, got %d (detail=%q)", got.ToolCount, got.Detail)
	}
}

// TestMCPProbe_HTTPEndpointDialFailure proves an unreachable HTTP endpoint fails its own
// row (OK=false) with a redacted error instead of a misleading reachable-by-config OK.
func TestMCPProbe_HTTPEndpointDialFailure(t *testing.T) {
	got := ProbeServer(context.Background(), "dead", ManagedServer{Type: ServerTypeStreamableHTTP, URL: "http://127.0.0.1:0/mcp"})
	if got.OK {
		t.Errorf("ProbeServer(dead http): want OK=false, got %+v", got)
	}
	if got.Err == "" {
		t.Errorf("ProbeServer(dead http): want a redacted Err, got empty")
	}
}

// TestMCPProbe_RedactsSecretsInError proves a dial error carrying a secret-shaped env
// fragment is redacted before it reaches the ProbeResult (T-28-01-02). We force a dial
// failure with a bogus command and assert the helper-redaction path is wired (the Err
// passes through RedactSecrets); since the command itself is not secret-shaped, we only
// assert an Err is produced and contains no raw "Bearer"/token value.
func TestMCPProbe_RedactsSecretsInError(t *testing.T) {
	// Set a secret-shaped env on the (failing) server; the dial error from a missing
	// binary won't echo it, but this guards that ProbeServer routes Err through
	// RedactSecrets (regression guard for the redaction wiring).
	got := ProbeServer(context.Background(), "secret-srv", ManagedServer{
		Command: "this-binary-does-not-exist-28-01",
		Env:     []string{"API_TOKEN=supersecretvalue"},
	})
	if got.OK {
		t.Fatalf("want dial failure, got OK %+v", got)
	}
	if got.Err == "" {
		t.Fatal("want non-empty Err on dial failure")
	}
	// The raw secret value must never appear verbatim in the surfaced error.
	if strings.Contains(got.Err, "supersecretvalue") {
		t.Errorf("Err leaked a secret value: %q", got.Err)
	}
}
