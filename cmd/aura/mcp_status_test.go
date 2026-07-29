package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/mcp"
)

// newMCPHTTPTestServerWithTools is newMCPHTTPTestServer's zero-or-more-tools sibling:
// same initialize/notifications/tools-list handshake, but the tools/list result is
// caller-supplied so a zero-tools reachable server (OK=true, ToolCount 0) can be
// distinguished from the fixed one-tool "echo" fixture.
func newMCPHTTPTestServerWithTools(t *testing.T, tools []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var req struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "session-status-test")
			writeMCPHTTPResult(t, w, req.ID, map[string]any{"protocolVersion": "2025-06-18"})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			writeMCPHTTPResult(t, w, req.ID, map[string]any{"tools": tools})
		default:
			t.Fatalf("unexpected method %q", req.Method)
		}
	}))
}

// TestWriteRuntimeCheckDeadHTTPEndpointReportsNotOK is the F-046 regression: a
// dead/typoed streamable-HTTP endpoint must report "runtime missing", never the old
// false-healthy "http endpoint configured" short-circuit.
func TestWriteRuntimeCheckDeadHTTPEndpointReportsNotOK(t *testing.T) {
	var out bytes.Buffer
	server := mcp.ManagedServer{Type: mcp.ServerTypeStreamableHTTP, URL: "http://127.0.0.1:0/mcp"}
	if err := writeRuntimeCheck(context.Background(), &out, "deadhttp", server); err != nil {
		t.Fatalf("writeRuntimeCheck: %v", err)
	}
	got := out.String()
	if strings.Contains(got, "http endpoint configured") {
		t.Fatalf("writeRuntimeCheck still uses the F-046 false-healthy short-circuit:\n%s", got)
	}
	if !strings.Contains(got, "deadhttp: runtime missing") {
		t.Fatalf("writeRuntimeCheck(dead http) = %q, want a runtime-missing line", got)
	}
}

// TestWriteRuntimeCheckReachableHTTPEndpointOK proves a live HTTP MCP endpoint
// probes OK via mcp.ProbeServer (the stdio branch already did; this is the fix).
func TestWriteRuntimeCheckReachableHTTPEndpointOK(t *testing.T) {
	srv := newMCPHTTPTestServer(t)
	defer srv.Close()

	var out bytes.Buffer
	server := mcp.ManagedServer{Type: mcp.ServerTypeStreamableHTTP, URL: srv.URL}
	if err := writeRuntimeCheck(context.Background(), &out, "livehttp", server); err != nil {
		t.Fatalf("writeRuntimeCheck: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "livehttp: runtime ok") {
		t.Fatalf("writeRuntimeCheck(reachable http) = %q, want a runtime-ok line", got)
	}
}

func TestStrictRuntimeProfileOwnsManagedMCPEgress(t *testing.T) {
	for _, profile := range []string{"single_user_hardened", "server_production"} {
		t.Run(profile, func(t *testing.T) {
			t.Setenv("AURA_PROFILE", profile)
			t.Setenv("AURA_MCP_SSRF_ENFORCE", "false")

			srv := newMCPHTTPTestServer(t)
			defer srv.Close()
			remote := mcp.ManagedServer{
				Type:   mcp.ServerTypeStreamableHTTP,
				URL:    srv.URL,
				Source: "operator:remote",
				Trust:  mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP},
			}
			if policy := runtimeMCPEgressPolicy(remote); !policy.Enforced() {
				t.Fatal("strict runtime profile was weakened by AURA_MCP_SSRF_ENFORCE=false")
			}
			if _, err := openManagedMCPTransport(context.Background(), "remote", remote); err == nil || !strings.Contains(err.Error(), "blocked target") {
				t.Fatalf("strict remote/private open error = %v, want SSRF rejection", err)
			}

			recipe := remote
			recipe.Source = "recipe:calendar"
			recipe.Trust = mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe}
			client, err := openManagedMCPTransport(context.Background(), "calendar", recipe)
			if err != nil {
				t.Fatalf("exact trusted recipe destination rejected under strict profile: %v", err)
			}
			if err := client.Close(); err != nil {
				t.Fatalf("Close trusted recipe: %v", err)
			}
		})
	}
}

// TestWriteRuntimeCheckZeroToolsHTTPEndpointOK proves a reachable HTTP server with
// zero advertised tools still probes OK=true (ToolCount 0 is not a failure).
func TestWriteRuntimeCheckZeroToolsHTTPEndpointOK(t *testing.T) {
	srv := newMCPHTTPTestServerWithTools(t, []map[string]any{})
	defer srv.Close()

	var out bytes.Buffer
	server := mcp.ManagedServer{Type: mcp.ServerTypeStreamableHTTP, URL: srv.URL}
	if err := writeRuntimeCheck(context.Background(), &out, "zerotools", server); err != nil {
		t.Fatalf("writeRuntimeCheck: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "zerotools: runtime ok") {
		t.Fatalf("writeRuntimeCheck(zero-tools http) = %q, want a runtime-ok line", got)
	}
}

// TestWriteRuntimeCheckBoundedByProbeTimeout proves a hung HTTP endpoint returns
// within ~AURA_MCP_PROBE_TIMEOUT instead of blocking indefinitely. The handler
// self-bounds its own block to a fixed 3s fallback (instead of ONLY waiting on
// r.Context().Done()): a client-side context.WithTimeout cancellation does not
// reliably close the underlying TCP connection promptly on every platform (this
// dev box observed net/http's Windows-side teardown taking many seconds longer
// than the client-visible cancellation), which would otherwise make httptest's
// own server.Close() — not the probe under test — hang for the diagnostic's
// duration. The probe itself (asserted below) already returns in ~1s regardless.
func TestWriteRuntimeCheckBoundedByProbeTimeout(t *testing.T) {
	t.Setenv("AURA_MCP_PROBE_TIMEOUT", "1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	defer srv.Close()

	var out bytes.Buffer
	server := mcp.ManagedServer{Type: mcp.ServerTypeStreamableHTTP, URL: srv.URL}
	start := time.Now()
	if err := writeRuntimeCheck(context.Background(), &out, "hung", server); err != nil {
		t.Fatalf("writeRuntimeCheck: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed > 5*time.Second {
		t.Fatalf("writeRuntimeCheck(hung http) took %v, want bounded by ~1s AURA_MCP_PROBE_TIMEOUT", elapsed)
	}
	if got := out.String(); !strings.Contains(got, "hung: runtime missing") {
		t.Fatalf("writeRuntimeCheck(hung http) = %q, want a runtime-missing line", got)
	}
}

// TestMCPStatusReflectsLiveHTTPProbe proves `aura mcp status` (D-17) surfaces the
// live probe result additively alongside SnapshotStatus's config-derived columns:
// a reachable HTTP server's probe column reports ok, a dead one reports a failure —
// neither replaces the trust/runtime/profiles columns already there.
func TestMCPStatusReflectsLiveHTTPProbe(t *testing.T) {
	path := withTempMCPConfig(t)
	srv := newMCPHTTPTestServer(t)
	defer srv.Close()

	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"live": {Type: mcp.ServerTypeStreamableHTTP, URL: srv.URL, Source: "manual:http", Trust: mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP}},
		"dead": {Type: mcp.ServerTypeStreamableHTTP, URL: "http://127.0.0.1:0/mcp", Source: "manual:http", Trust: mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP}},
	}}
	if err := mcp.SaveManagedConfig(path, doc); err != nil {
		t.Fatalf("save config: %v", err)
	}

	var out bytes.Buffer
	if err := runMCPCommand(context.Background(), nil, []string{"status", "--json"}, &out); err != nil {
		t.Fatalf("mcp status --json: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, `"name":"live"`) || !strings.Contains(got, `"trust":"remote_http"`) {
		t.Fatalf("status json missing config-derived columns for live:\n%s", got)
	}
	if !strings.Contains(got, `"probe":{"name":"live","ok":true`) {
		t.Fatalf("status json missing live-probe OK=true for reachable server:\n%s", got)
	}
	if !strings.Contains(got, `"name":"dead"`) {
		t.Fatalf("status json missing dead server row:\n%s", got)
	}
	if strings.Contains(got, `"probe":{"name":"dead","ok":true`) {
		t.Fatalf("status json reports OK=true for a dead HTTP endpoint:\n%s", got)
	}
}

// TestMCPStatusSkipsProbingDisabledAndBlockedServers proves probeStatusRow never
// dials a disabled or trust-blocked server as a side effect of a read-only `status`
// listing (Rule 2 addition: mirrors mcpDoctorAll's own disabled/blocked skip so a
// blocked stdio command is never spawned just to render a status table).
func TestMCPStatusSkipsProbingDisabledAndBlockedServers(t *testing.T) {
	path := withTempMCPConfig(t)
	disabled := false
	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"off":     {Command: "aura-nonexistent-mcp-binary-xyz", Source: "manual", Enabled: &disabled, Trust: mcp.ManagedTrust{Class: mcp.TrustTrustedLocal}},
		"blocked": {Command: "aura-nonexistent-mcp-binary-xyz", Source: "manual", Trust: mcp.ManagedTrust{Class: mcp.TrustBlocked}},
	}}
	if err := mcp.SaveManagedConfig(path, doc); err != nil {
		t.Fatalf("save config: %v", err)
	}

	var out bytes.Buffer
	if err := runMCPCommand(context.Background(), nil, []string{"status", "--json"}, &out); err != nil {
		t.Fatalf("mcp status --json: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, `"probe":{"name":"off","ok":false,"tool_count":0,"detail":"skipped (disabled)"}`) {
		t.Fatalf("status json did not skip probing the disabled server:\n%s", got)
	}
	if !strings.Contains(got, `"probe":{"name":"blocked","ok":false,"tool_count":0,"detail":"skipped (blocked)"}`) {
		t.Fatalf("status json did not skip probing the blocked server:\n%s", got)
	}
}
