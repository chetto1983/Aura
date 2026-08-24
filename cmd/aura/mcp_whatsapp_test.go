package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
)

func TestMCPDoctorWhatsAppReportsBridgeHealth(t *testing.T) {
	withMemoryMCPRegistry(t)
	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"whatsapp": {
			Command: os.Args[0],
			Args:    []string{"-test.run=TestMCPServerHelperProcess", "--"},
			Env:     []string{"AURA_MCP_HELPER=1"},
			Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
			Source:  "recipe:whatsapp",
		},
	}}
	seedMCPRegistry(t, doc)

	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/status" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"state":"waiting_qr","paired":false,"qr_available":true}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer bridge.Close()
	t.Setenv("AURA_MCP_WHATSAPP_BRIDGE_URL", bridge.URL)

	var out bytes.Buffer
	if err := runMCPCommand(context.Background(), nil, []string{"doctor", "whatsapp"}, &out); err != nil {
		t.Fatalf("mcp doctor whatsapp: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"ok: whatsapp started; 1 tool",
		"whatsapp bridge: REST",
		"reachable (GET /api/status -> state=waiting_qr, paired=false, qr_available=true)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, got)
		}
	}
}

func TestMCPDoctorWhatsAppHTTPRecipeReportsBridgeHealth(t *testing.T) {
	withMemoryMCPRegistry(t)
	server := newMCPHTTPTestServer(t)
	defer server.Close()
	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"whatsapp": {
			Type:   mcp.ServerTypeStreamableHTTP,
			URL:    server.URL,
			Trust:  mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
			Source: "recipe:whatsapp",
		},
	}}
	seedMCPRegistry(t, doc)

	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/status" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"state":"connected","paired":true,"qr_available":false,"jid":"123@s.whatsapp.net"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer bridge.Close()
	t.Setenv("AURA_MCP_WHATSAPP_BRIDGE_URL", bridge.URL)

	var out bytes.Buffer
	if err := runMCPCommand(context.Background(), nil, []string{"doctor", "whatsapp"}, &out); err != nil {
		t.Fatalf("mcp doctor whatsapp HTTP recipe: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"ok: whatsapp started; 1 tool",
		"whatsapp bridge: REST",
		"reachable (GET /api/status -> state=connected, paired=true, qr_available=false, jid=123@s.whatsapp.net)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "command cannot be empty") {
		t.Fatalf("doctor tried to launch an HTTP recipe as stdio:\n%s", got)
	}
}

func TestProbeWhatsAppBridgeUsesWSLForWSLRecipe(t *testing.T) {
	orig := runWhatsAppBridgeWSLProbe
	t.Cleanup(func() { runWhatsAppBridgeWSLProbe = orig })

	var gotCfg mcp.ServerConfig
	runWhatsAppBridgeWSLProbe = func(_ context.Context, cfg mcp.ServerConfig) (int, error) {
		gotCfg = cfg
		return http.StatusMethodNotAllowed, nil
	}

	status := probeWhatsAppBridge(context.Background(), mcp.ServerConfig{
		Command: "wsl.exe",
		Args:    []string{"-d", "Ubuntu", "-e", "bash", "-lc", "cd ~/whatsapp-mcp/whatsapp-mcp-server && uv run main.py"},
	})
	if !strings.Contains(status, "REST :8080 in WSL reachable (GET /api/send -> 405)") {
		t.Fatalf("status = %q", status)
	}
	if gotCfg.Command != "wsl.exe" || strings.Join(gotCfg.Args[:2], " ") != "-d Ubuntu" {
		t.Fatalf("WSL probe did not receive original distro context: %#v", gotCfg)
	}
}
