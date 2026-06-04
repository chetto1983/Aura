package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
)

func withTempMCPConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "servers.json")
	t.Setenv("AURA_MCP_CONFIG", path)
	return path
}

func TestMCPInstallCalculatorWritesRecipe(t *testing.T) {
	path := withTempMCPConfig(t)

	var out bytes.Buffer
	if err := runMCPCommand(context.Background(), []string{"install", "calculator"}, &out); err != nil {
		t.Fatalf("mcp install calculator: %v", err)
	}

	doc, err := mcp.LoadManagedConfig(path)
	if err != nil {
		t.Fatalf("load managed config: %v", err)
	}
	calc := doc.MCPServers["calculator"]
	if calc.Command != "uvx" {
		t.Fatalf("calculator command = %q, want uvx", calc.Command)
	}
	if strings.Join(calc.Args, " ") != "--from calculator-mcp-server@git+https://github.com/chetto1983/calculator-mcp-server.git -- calculator-mcp-server --stdio" {
		t.Fatalf("calculator args not recipe-shaped: %#v", calc.Args)
	}
	if !strings.Contains(out.String(), "ok: installed calculator") {
		t.Fatalf("install output missing success line:\n%s", out.String())
	}
}

func TestMCPRecipesListsBuiltins(t *testing.T) {
	withTempMCPConfig(t)

	var out bytes.Buffer
	if err := runMCPCommand(context.Background(), []string{"recipes"}, &out); err != nil {
		t.Fatalf("mcp recipes: %v", err)
	}
	got := out.String()
	for _, want := range []string{"calculator", "mail", "whatsapp", "calendar", "trusted_recipe"} {
		if !strings.Contains(got, want) {
			t.Fatalf("recipes output missing %q:\n%s", want, got)
		}
	}

	out.Reset()
	if err := runMCPCommand(context.Background(), []string{"recipes", "--json"}, &out); err != nil {
		t.Fatalf("mcp recipes --json: %v", err)
	}
	if got := out.String(); !strings.Contains(got, `"name":"calendar"`) || !strings.Contains(got, `"AURA_CALENDAR_MODE=fixture"`) {
		t.Fatalf("recipes json missing calendar fixture metadata:\n%s", got)
	}
}

func TestMCPAddListAndDisable(t *testing.T) {
	path := withTempMCPConfig(t)

	var out bytes.Buffer
	err := runMCPCommand(context.Background(), []string{"add", "local", "--env", "TOKEN=secret", "--", "python", "server.py", "--stdio"}, &out)
	if err != nil {
		t.Fatalf("mcp add: %v", err)
	}
	doc, err := mcp.LoadManagedConfig(path)
	if err != nil {
		t.Fatalf("load managed config: %v", err)
	}
	if got := doc.MCPServers["local"].Trust.Class; got != mcp.TrustBlocked {
		t.Fatalf("manual add trust = %q, want %q", got, mcp.TrustBlocked)
	}
	if got := doc.ProfileServerNames(mcp.DefaultMCPProfile); !containsString(got, "local") {
		t.Fatalf("default profile missing local server: %#v", got)
	}
	out.Reset()
	if err := runMCPCommand(context.Background(), []string{"list"}, &out); err != nil {
		t.Fatalf("mcp list: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "local") || !strings.Contains(got, "python server.py --stdio") {
		t.Fatalf("list output missing local command:\n%s", got)
	}

	out.Reset()
	if err := runMCPCommand(context.Background(), []string{"disable", "local"}, &out); err != nil {
		t.Fatalf("mcp disable: %v", err)
	}
	out.Reset()
	if err := runMCPCommand(context.Background(), []string{"list"}, &out); err != nil {
		t.Fatalf("mcp list after disable: %v", err)
	}
	if !strings.Contains(out.String(), "disabled") {
		t.Fatalf("disabled server status not rendered:\n%s", out.String())
	}
}

func TestMCPProfileCommands(t *testing.T) {
	path := withTempMCPConfig(t)

	var out bytes.Buffer
	if err := runMCPCommand(context.Background(), []string{"profile", "create", "work"}, &out); err != nil {
		t.Fatalf("profile create: %v", err)
	}
	out.Reset()
	if err := runMCPCommand(context.Background(), []string{"profile", "use", "work"}, &out); err != nil {
		t.Fatalf("profile use: %v", err)
	}
	out.Reset()
	if err := runMCPCommand(context.Background(), []string{"install", "calendar"}, &out); err != nil {
		t.Fatalf("install calendar: %v", err)
	}
	doc, err := mcp.LoadManagedConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if doc.ActiveProfileName() != "work" {
		t.Fatalf("active profile = %q, want work", doc.ActiveProfileName())
	}
	if got := doc.ProfileServerNames("work"); !containsString(got, "calendar") {
		t.Fatalf("work profile missing calendar: %#v", got)
	}

	out.Reset()
	if err := runMCPCommand(context.Background(), []string{"profile", "remove", "work", "calendar"}, &out); err != nil {
		t.Fatalf("profile remove: %v", err)
	}
	doc, err = mcp.LoadManagedConfig(path)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if got := doc.ProfileServerNames("work"); containsString(got, "calendar") {
		t.Fatalf("work profile still has calendar: %#v", got)
	}
}

func TestMCPTrustRecordsApproval(t *testing.T) {
	path := withTempMCPConfig(t)

	var out bytes.Buffer
	if err := runMCPCommand(context.Background(), []string{"add", "local", "--", "node", "server.js"}, &out); err != nil {
		t.Fatalf("mcp add: %v", err)
	}
	out.Reset()
	if err := runMCPCommand(context.Background(), []string{"trust", "local"}, &out); err != nil {
		t.Fatalf("mcp trust: %v", err)
	}
	doc, err := mcp.LoadManagedConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got := doc.MCPServers["local"].Trust.Class; got != mcp.TrustTrustedLocal {
		t.Fatalf("trust class = %q, want %q", got, mcp.TrustTrustedLocal)
	}
	if got := out.String(); !strings.Contains(got, "ok: trusted local as trusted_local") {
		t.Fatalf("trust output missing confirmation:\n%s", got)
	}
}

func TestMCPStatusJSONShowsBlockedServer(t *testing.T) {
	withTempMCPConfig(t)
	var out bytes.Buffer
	if err := runMCPCommand(context.Background(), []string{"add", "local", "--", "node", "server.js"}, &out); err != nil {
		t.Fatalf("mcp add: %v", err)
	}
	out.Reset()
	if err := runMCPCommand(context.Background(), []string{"status", "--json"}, &out); err != nil {
		t.Fatalf("mcp status --json: %v", err)
	}
	got := out.String()
	for _, want := range []string{`"name":"local"`, `"startupState":"blocked"`, `"trust":"blocked"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("status json missing %q:\n%s", want, got)
		}
	}
}

func TestMCPStatusShowsPolicyCountsAndRiskLabels(t *testing.T) {
	path := withTempMCPConfig(t)
	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"mail": {
			Command:    "npx",
			Source:     "recipe:mail",
			Trust:      mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
			RiskLabels: []string{mcpmanager.RiskPrivateData, mcpmanager.RiskExternalSend},
			ToolPolicy: mcp.ManagedToolPolicy{
				Allow:    []string{"send_email", "fetch_emails"},
				DenyRisk: []string{mcpmanager.RiskDestructive, mcpmanager.RiskUnknown},
			},
		},
	}}
	if err := mcp.SaveManagedConfig(path, doc); err != nil {
		t.Fatalf("save config: %v", err)
	}

	var out bytes.Buffer
	if err := runMCPCommand(context.Background(), []string{"status"}, &out); err != nil {
		t.Fatalf("mcp status: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "mounted\tblocked") || !strings.Contains(got, "mail") || !strings.Contains(got, "\t2\t2\t") {
		t.Fatalf("status output missing policy counts:\n%s", got)
	}

	out.Reset()
	if err := runMCPCommand(context.Background(), []string{"status", "--json"}, &out); err != nil {
		t.Fatalf("mcp status --json: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		`"riskLabels":["private_data","external_send"]`,
		`"mountedToolCount":2`,
		`"blockedToolCount":2`,
		`"risk":"destructive"`,
		`"risk":"unknown"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("status json missing %q:\n%s", want, got)
		}
	}
}

func TestMCPDoctorAllRedactsAndChecksRecipes(t *testing.T) {
	path := withTempMCPConfig(t)
	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"mail": {
			Command: "npx",
			Env:     []string{"SMTP_USER=me@example.com", "SMTP_PASS=super-secret"},
			Source:  "recipe:mail",
			Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
		},
		"calendar": {
			Command: "calendar-fixture",
			Env:     []string{"AURA_CALENDAR_MODE=fixture"},
			Source:  "recipe:calendar",
			Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
		},
	}}
	if err := mcp.SaveManagedConfig(path, doc); err != nil {
		t.Fatalf("save config: %v", err)
	}
	origLookPath := mcpLookPath
	t.Cleanup(func() { mcpLookPath = origLookPath })
	mcpLookPath = func(command string) (string, error) { return "/fake/" + command, nil }

	var out bytes.Buffer
	if err := runMCPCommand(context.Background(), []string{"doctor", "--all"}, &out); err != nil {
		t.Fatalf("mcp doctor --all: %v", err)
	}
	got := out.String()
	for _, want := range []string{"mail: runtime ok", "mail env SMTP_PASS=<redacted>", "calendar fixture: ready"} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor --all missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "super-secret") {
		t.Fatalf("doctor leaked secret:\n%s", got)
	}
}

func TestMCPDoctorBlockedDoesNotLaunch(t *testing.T) {
	path := withTempMCPConfig(t)
	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"blocked": {Command: "aura-nonexistent-mcp-binary-xyz", Source: "manual", Trust: mcp.ManagedTrust{Class: mcp.TrustBlocked}},
	}}
	if err := mcp.SaveManagedConfig(path, doc); err != nil {
		t.Fatalf("save config: %v", err)
	}
	var out bytes.Buffer
	if err := runMCPCommand(context.Background(), []string{"doctor", "blocked"}, &out); err != nil {
		t.Fatalf("doctor blocked: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "blocked: trust approval required") {
		t.Fatalf("doctor blocked output = %s", got)
	}
}

func TestMCPDoctorAndToolsStartConfiguredServer(t *testing.T) {
	path := withTempMCPConfig(t)
	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"calculator": {
			Command: os.Args[0],
			Args:    []string{"-test.run=TestMCPServerHelperProcess", "--"},
			Env:     []string{"AURA_MCP_HELPER=1"},
			Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
			Source:  "recipe:calculator",
		},
	}}
	if err := mcp.SaveManagedConfig(path, doc); err != nil {
		t.Fatalf("save managed config: %v", err)
	}

	var out bytes.Buffer
	if err := runMCPCommand(context.Background(), []string{"doctor", "calculator"}, &out); err != nil {
		t.Fatalf("mcp doctor: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "ok: calculator") || !strings.Contains(got, "1 tool") {
		t.Fatalf("doctor output missing ok/tools:\n%s", got)
	}

	out.Reset()
	if err := runMCPCommand(context.Background(), []string{"tools", "calculator"}, &out); err != nil {
		t.Fatalf("mcp tools: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "calculate") || !strings.Contains(got, "Evaluate a mathematical expression.") {
		t.Fatalf("tools output missing calculator tool:\n%s", got)
	}
}

func TestMCPManagerMockE2EProfileRecipeBlockedAndTools(t *testing.T) {
	path := withTempMCPConfig(t)
	var out bytes.Buffer
	if err := runMCPCommand(context.Background(), []string{"install", "calculator"}, &out); err != nil {
		t.Fatalf("install calculator: %v", err)
	}
	doc, err := mcp.LoadManagedConfig(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	calc := doc.MCPServers["calculator"]
	calc.Command = os.Args[0]
	calc.Args = []string{"-test.run=TestMCPServerHelperProcess", "--"}
	calc.Env = []string{"AURA_MCP_HELPER=1"}
	doc.MCPServers["calculator"] = calc
	if err := mcp.SaveManagedConfig(path, doc); err != nil {
		t.Fatalf("save fake calculator: %v", err)
	}

	out.Reset()
	if err := runMCPCommand(context.Background(), []string{"profile", "create", "e2e"}, &out); err != nil {
		t.Fatalf("profile create: %v", err)
	}
	out.Reset()
	if err := runMCPCommand(context.Background(), []string{"profile", "add", "e2e", "calculator"}, &out); err != nil {
		t.Fatalf("profile add: %v", err)
	}
	out.Reset()
	if err := runMCPCommand(context.Background(), []string{"profile", "use", "e2e"}, &out); err != nil {
		t.Fatalf("profile use: %v", err)
	}
	out.Reset()
	if err := runMCPCommand(context.Background(), []string{"add", "blocked", "--", "aura-nonexistent-mcp-binary-xyz"}, &out); err != nil {
		t.Fatalf("add blocked: %v", err)
	}

	out.Reset()
	if err := runMCPCommand(context.Background(), []string{"tools", "calculator"}, &out); err != nil {
		t.Fatalf("tools calculator: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "calculate\tread\tmounted\tEvaluate a mathematical expression.") {
		t.Fatalf("tools calculator output missing risk/mounted row:\n%s", got)
	}

	out.Reset()
	if err := runMCPCommand(context.Background(), []string{"status", "--json"}, &out); err != nil {
		t.Fatalf("status --json: %v", err)
	}
	got := out.String()
	for _, want := range []string{`"name":"blocked"`, `"startupState":"blocked"`, `"name":"calculator"`, `"profiles":["default","e2e"]`} {
		if !strings.Contains(got, want) {
			t.Fatalf("status json missing %q:\n%s", want, got)
		}
	}

	out.Reset()
	if err := runMCPCommand(context.Background(), []string{"doctor", "blocked"}, &out); err != nil {
		t.Fatalf("doctor blocked: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "blocked: trust approval required") {
		t.Fatalf("doctor blocked should report trust gate without launching:\n%s", got)
	}
}

func TestMCPToolsSupportsManagedStreamableHTTPServer(t *testing.T) {
	path := withTempMCPConfig(t)
	server := newMCPHTTPTestServer(t)
	defer server.Close()

	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"remote": {
			Type:   mcp.ServerTypeStreamableHTTP,
			URL:    server.URL,
			Source: "manual:http",
			Trust:  mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP},
			ToolPolicy: mcp.ManagedToolPolicy{
				Allow: []string{"echo"},
			},
		},
	}}
	if err := mcp.SaveManagedConfig(path, doc); err != nil {
		t.Fatalf("save managed config: %v", err)
	}

	var out bytes.Buffer
	if err := runMCPCommand(context.Background(), []string{"tools", "remote"}, &out); err != nil {
		t.Fatalf("mcp tools remote: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "echo\tread\tmounted\tGet echo text.") {
		t.Fatalf("tools output missing remote HTTP tool:\n%s", got)
	}
}

func TestMCPToolsShowsRiskLabelsAndBlockedTools(t *testing.T) {
	path := withTempMCPConfig(t)
	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"mail": {
			Command: os.Args[0],
			Args:    []string{"-test.run=TestMCPServerHelperProcess", "--"},
			Env:     []string{"AURA_MCP_HELPER=1", "AURA_MCP_HELPER_TOOLS=policy"},
			Source:  "recipe:mail",
			Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
			RiskLabels: []string{
				mcpmanager.RiskPrivateData,
			},
			ToolPolicy: mcp.ManagedToolPolicy{
				Allow:    []string{"send_email", "fetch_emails", "delete_mailbox", "mystery"},
				DenyRisk: []string{mcpmanager.RiskDestructive, mcpmanager.RiskUnknown},
			},
		},
	}}
	if err := mcp.SaveManagedConfig(path, doc); err != nil {
		t.Fatalf("save managed config: %v", err)
	}

	var out bytes.Buffer
	if err := runMCPCommand(context.Background(), []string{"tools", "mail"}, &out); err != nil {
		t.Fatalf("mcp tools: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"send_email\texternal_send,private_data,write\tmounted",
		"fetch_emails\tprivate_data,read\tmounted",
		"delete_mailbox\tdestructive,private_data,write\tblocked: risk destructive denied",
		"mystery\tprivate_data,unknown\tblocked: risk unknown denied",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("tools output missing %q:\n%s", want, got)
		}
	}
}

func TestMCPDoctorWhatsAppReportsBridgeHealth(t *testing.T) {
	path := withTempMCPConfig(t)
	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"whatsapp": {
			Command: os.Args[0],
			Args:    []string{"-test.run=TestMCPServerHelperProcess", "--"},
			Env:     []string{"AURA_MCP_HELPER=1"},
			Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
			Source:  "recipe:whatsapp",
		},
	}}
	if err := mcp.SaveManagedConfig(path, doc); err != nil {
		t.Fatalf("save managed config: %v", err)
	}

	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api/send" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		http.NotFound(w, r)
	}))
	defer bridge.Close()
	t.Setenv("AURA_MCP_WHATSAPP_BRIDGE_URL", bridge.URL)

	var out bytes.Buffer
	if err := runMCPCommand(context.Background(), []string{"doctor", "whatsapp"}, &out); err != nil {
		t.Fatalf("mcp doctor whatsapp: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"ok: whatsapp started; 1 tool",
		"whatsapp bridge: REST",
		"reachable (GET /api/send -> 405)",
		"connected-state unavailable (bridge exposes no status endpoint)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, got)
		}
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

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func newMCPHTTPTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
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
			w.Header().Set("Mcp-Session-Id", "session-remote")
			writeMCPHTTPResult(t, w, req.ID, map[string]any{"protocolVersion": "2025-06-18"})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			writeMCPHTTPResult(t, w, req.ID, map[string]any{"tools": []map[string]any{{
				"name":        "echo",
				"description": "Get echo text.",
				"inputSchema": map[string]any{"type": "object"},
			}}})
		case "tools/call":
			writeMCPHTTPResult(t, w, req.ID, map[string]any{
				"content": []map[string]any{{"type": "text", "text": "pong"}},
			})
		default:
			t.Fatalf("unexpected method %q", req.Method)
		}
	}))
}

func writeMCPHTTPResult(t *testing.T, w http.ResponseWriter, id int64, result any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	}); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}
