package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/mcp"
)

// mcpFakeServer spawns an in-memory MCP HTTP server that advertises one
// tool per call so the api/mcp endpoint has something to serialize.
func mcpFakeServer(t *testing.T, toolName, toolDesc string) *mcp.Client {
	return mcpFakeServerNamed(t, "srv1", toolName, toolDesc)
}

func mcpFakeServerNamed(t *testing.T, name, toolName, toolDesc string) *mcp.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var probe struct {
			Method string          `json:"method"`
			ID     json.RawMessage `json:"id,omitempty"`
		}
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		_ = json.Unmarshal(body, &probe)
		w.Header().Set("Content-Type", "application/json")
		switch probe.Method {
		case "initialize":
			w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(probe.ID) + `,"result":{"capabilities":{}}}`))
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			tools, _ := json.Marshal([]mcp.Tool{{
				Name:        toolName,
				Description: toolDesc,
				InputSchema: map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}},
			}})
			w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(probe.ID) + `,"result":{"tools":` + string(tools) + `}}`))
		}
	}))
	t.Cleanup(srv.Close)

	client, err := mcp.NewHTTPClient(name, srv.URL, nil)
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestMCPServers_Empty(t *testing.T) {
	router := NewRouter(Deps{})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/mcp/servers", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var got []MCPServerSummary
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty list, got %+v", got)
	}
}

func TestMCPServers_ReturnsToolMetadata(t *testing.T) {
	c := mcpFakeServer(t, "search", "Search the index")
	router := NewRouter(Deps{MCP: []mcp.ConnectedClient{c}})

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/mcp/servers", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var got []MCPServerSummary
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 server, got %d", len(got))
	}
	srv := got[0]
	if srv.Name != "srv1" {
		t.Errorf("name = %q, want srv1", srv.Name)
	}
	if srv.Transport != mcp.TransportHTTP {
		t.Errorf("transport = %q, want %q", srv.Transport, mcp.TransportHTTP)
	}
	if srv.ToolCount != 1 || len(srv.Tools) != 1 {
		t.Fatalf("expected 1 tool, got count=%d tools=%+v", srv.ToolCount, srv.Tools)
	}
	tool := srv.Tools[0]
	if tool.Name != "search" {
		t.Errorf("tool name = %q", tool.Name)
	}
	if tool.Description != "Search the index" {
		t.Errorf("tool desc = %q", tool.Description)
	}
	if tool.InputSchema == nil {
		t.Errorf("input_schema missing")
	}
}

func TestMCPStatus_ReturnsOverrideWarnings(t *testing.T) {
	c := mcpFakeServer(t, "search", "Search the index")
	mcp.ApplyToolDescriptionOverrides([]mcp.ConnectedClient{c}, map[string]string{
		"mcp_srv1_search": "Search with local operator context",
		"typo_key":        "Does not match",
	})
	router := NewRouter(Deps{MCP: []mcp.ConnectedClient{c}})

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/mcp/status", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var got []MCPServerSummary
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 server, got %d", len(got))
	}
	if got[0].Tools[0].Description != "Search with local operator context" {
		t.Fatalf("description = %q", got[0].Tools[0].Description)
	}
	if len(got[0].OverrideWarnings) != 1 || got[0].OverrideWarnings[0] != "typo_key" {
		t.Fatalf("override warnings = %#v", got[0].OverrideWarnings)
	}
}

func TestMCPServers_HandlesNilClient(t *testing.T) {
	router := NewRouter(Deps{MCP: []mcp.ConnectedClient{nil}})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/mcp/servers", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var got []MCPServerSummary
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if len(got) != 0 {
		t.Fatalf("nil client should be skipped, got %+v", got)
	}
}

func TestMCPProviders_ReturnsMailAndDatabaseProfiles(t *testing.T) {
	router := NewRouter(Deps{})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/mcp/providers", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}

	var got []ConnectorProviderSummary
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("providers = %d, want 2: %+v", len(got), got)
	}

	var mailFound, databaseFound bool
	for _, p := range got {
		switch p.ID {
		case "mail-mcp":
			mailFound = true
			if p.Kind != "mail" {
				t.Errorf("mail-mcp kind = %q, want mail", p.Kind)
			}
			if len(p.Capabilities) == 0 {
				t.Errorf("mail-mcp capabilities empty")
			}
		case "database":
			databaseFound = true
			if p.Kind != "database" {
				t.Errorf("database kind = %q, want database", p.Kind)
			}
			if !containsString(p.ApprovedTools, "read_query") {
				t.Errorf("database approved tools missing read_query: %+v", p.ApprovedTools)
			}
			if !containsString(p.BlockedTools, "drop_table") {
				t.Errorf("database blocked tools missing drop_table: %+v", p.BlockedTools)
			}
		}
	}
	if !mailFound {
		t.Fatalf("mail-mcp provider missing from %+v", got)
	}
	if !databaseFound {
		t.Fatalf("database provider missing from %+v", got)
	}
}

func TestMCPProviders_ReflectsUserEnabledMailTools(t *testing.T) {
	dir := t.TempDir()
	mcpPath := filepath.Join(dir, "mcp.json")
	cfg := `{"mcpServers":{"mail":{"command":"/usr/local/bin/mail-mcp","env":{"MAIL_IMAP_DAVIDE_USER":"me@example.com","MAIL_IMAP_DAVIDE_PASS":"secret","MAIL_SMTP_DAVIDE_USER":"me@example.com","AURA_MAIL_DAVIDE_ENABLE_IMAP_MUTATIONS":"true"}}}}`
	if err := os.WriteFile(mcpPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(Deps{
		RuntimeConfig: &config.Config{MCPServersPath: mcpPath, RuntimeWorkspacePath: "/workspace"},
		MCP:           []mcp.ConnectedClient{mcpFakeServerNamed(t, "mail", "imap_search_messages", "Search mail")},
	})

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/mcp/providers", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var got []ConnectorProviderSummary
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	mail, ok := findProviderForTest(got, "mail-mcp")
	if !ok {
		t.Fatalf("mail provider missing: %+v", got)
	}
	if mail.Status != "enabled" {
		t.Fatalf("mail status = %q, want enabled", mail.Status)
	}
	if !capabilityEnabled(mail.Capabilities, "mail.search") {
		t.Fatalf("mail search capability should be enabled: %+v", mail.Capabilities)
	}
	if !capabilityEnabled(mail.Capabilities, "mail.draft_reply") {
		t.Fatalf("mail draft capability should be enabled when SMTP is enabled: %+v", mail.Capabilities)
	}
	if !containsString(mail.ApprovedTools, "smtp_send_message") {
		t.Fatalf("SMTP tool not enabled: %+v", mail.ApprovedTools)
	}
	if !containsString(mail.ApprovedTools, "imap_delete_message") {
		t.Fatalf("IMAP mutation tool not enabled: %+v", mail.ApprovedTools)
	}
	if containsString(mail.BlockedTools, "smtp_send_message") || containsString(mail.BlockedTools, "imap_delete_message") {
		t.Fatalf("enabled tools still listed as blocked: %+v", mail.BlockedTools)
	}
}

func TestMCPProviders_DoesNotLeakDynamicMailState(t *testing.T) {
	dir := t.TempDir()
	mcpPath := filepath.Join(dir, "mcp.json")
	cfg := `{"mcpServers":{"mail":{"command":"/usr/local/bin/mail-mcp","env":{"MAIL_IMAP_DAVIDE_USER":"me@example.com","MAIL_IMAP_DAVIDE_PASS":"secret","MAIL_SMTP_DAVIDE_USER":"me@example.com","AURA_MAIL_DAVIDE_ENABLE_IMAP_MUTATIONS":"true"}}}}`
	if err := os.WriteFile(mcpPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	enabled := connectorProviderSummaries(Deps{
		RuntimeConfig: &config.Config{MCPServersPath: mcpPath, RuntimeWorkspacePath: "/workspace"},
		MCP:           []mcp.ConnectedClient{mcpFakeServerNamed(t, "mail", "imap_search_messages", "Search mail")},
	})
	if mail, ok := findProviderForTest(enabled, "mail-mcp"); !ok || !capabilityEnabled(mail.Capabilities, "mail.search") {
		t.Fatalf("setup provider should be enabled: %+v", enabled)
	}

	defaults := connectorProviderSummaries(Deps{})
	mail, ok := findProviderForTest(defaults, "mail-mcp")
	if !ok {
		t.Fatalf("mail provider missing: %+v", defaults)
	}
	if mail.Status != "not_configured" {
		t.Fatalf("mail status leaked = %q, want not_configured", mail.Status)
	}
	if capabilityEnabled(mail.Capabilities, "mail.search") {
		t.Fatalf("mail search capability leaked enabled state: %+v", mail.Capabilities)
	}
}

func TestApplyDefaultMailAccountUsesConfiguredAccount(t *testing.T) {
	dir := t.TempDir()
	mcpPath := filepath.Join(dir, "mcp.json")
	cfg := `{"mcpServers":{"mail":{"command":"/usr/local/bin/mail-mcp","env":{"MAIL_IMAP_DAVIDE_USER":"me@example.com","MAIL_IMAP_DAVIDE_PASS":"secret"}}}}`
	if err := os.WriteFile(mcpPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	args := map[string]any{"mailbox": "INBOX"}
	applyDefaultMailAccount(Deps{RuntimeConfig: &config.Config{MCPServersPath: mcpPath}}, args)

	if args["account_id"] != "davide" {
		t.Fatalf("account_id = %v, want davide", args["account_id"])
	}
}

func findProviderForTest(values []ConnectorProviderSummary, id string) (ConnectorProviderSummary, bool) {
	for _, v := range values {
		if v.ID == id {
			return v, true
		}
	}
	return ConnectorProviderSummary{}, false
}

func capabilityEnabled(values []ConnectorCapability, id string) bool {
	for _, v := range values {
		if v.ID == id {
			return v.Enabled
		}
	}
	return false
}

func containsString(values []string, needle string) bool {
	for _, v := range values {
		if v == needle {
			return true
		}
	}
	return false
}
