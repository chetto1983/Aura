package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/mcp"
)

func TestMCPMailSetupSaveWritesMailMCPConfig(t *testing.T) {
	dir := t.TempDir()
	mcpPath := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(mcpPath, []byte(`{"mcpServers":{"database":{"url":"http://db.example/mcp"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(Deps{RuntimeConfig: &config.Config{
		MCPServersPath:       mcpPath,
		RuntimeWorkspacePath: "/workspace",
	}})

	rr := postRaw(t, router, "/mcp/setup/mail", `{
		"provider":"gmail",
		"account_id":"default",
		"email":"me@gmail.com",
		"app_password":"app-secret",
		"enable_smtp":true
	}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var resp MailSetupResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK || !resp.Status.Configured || !resp.Status.NeedsRestart {
		t.Fatalf("unexpected response: %+v", resp)
	}

	servers, err := mcp.LoadServers(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := servers["database"]; !ok {
		t.Fatalf("existing MCP server was not preserved: %+v", servers)
	}
	mail := servers["mail"]
	if mail.Command != "/usr/local/bin/mail-mcp" {
		t.Fatalf("mail command = %q", mail.Command)
	}
	for _, want := range []string{
		"MAIL_IMAP_DEFAULT_HOST=imap.gmail.com",
		"MAIL_IMAP_DEFAULT_USER=me@gmail.com",
		"MAIL_IMAP_DEFAULT_PASS=app-secret",
		"MAIL_SMTP_DEFAULT_HOST=smtp.gmail.com",
	} {
		key, value, _ := strings.Cut(want, "=")
		if mail.Env[key] != value {
			t.Fatalf("%s = %q, want %q", key, mail.Env[key], value)
		}
	}
	if strings.Contains(rr.Body.String(), "app-secret") {
		t.Fatalf("setup response leaked password: %s", rr.Body.String())
	}
}

func TestMCPMailSetupStatusReportsConnectedAlias(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "mail-mcp")
	if err := os.WriteFile(bin, []byte("stub"), 0o700); err != nil {
		t.Fatal(err)
	}
	mcpPath := filepath.Join(dir, "mcp.json")
	cfg := `{"mcpServers":{"mail":{"command":` + strconvQuote(bin) + `,"env":{"MAIL_IMAP_WORK_USER":"team@example.com"}}}}`
	if err := os.WriteFile(mcpPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	srv, state := newFakeMCPServer(t, []mcp.Tool{
		{Name: "imap_search_messages"},
		{Name: "imap_get_message"},
	})
	router := newInvokeRouter(t, "mail", state, srv)
	// Recreate with runtime config while reusing the fake connected client.
	router = NewRouter(Deps{
		RuntimeConfig: &config.Config{MCPServersPath: mcpPath, RuntimeWorkspacePath: dir},
		MCP:           []mcp.ConnectedClient{routerMCPClient(t, "mail", srv.URL)},
	})

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/mcp/setup/mail", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var got MailSetupStatus
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Configured || !got.Connected || got.NeedsRestart || !got.BinaryPresent {
		t.Fatalf("unexpected status: %+v", got)
	}
	if got.ConfiguredEmail != "team@example.com" || got.AccountID != "work" {
		t.Fatalf("unexpected account info: %+v", got)
	}
}

func TestMCPMailSetupRejectsUnsafeFields(t *testing.T) {
	router := NewRouter(Deps{RuntimeConfig: &config.Config{
		MCPServersPath:       filepath.Join(t.TempDir(), "mcp.json"),
		RuntimeWorkspacePath: "/workspace",
	}})
	rr := postRaw(t, router, "/mcp/setup/mail", `{"provider":"gmail","email":"me@gmail.com","app_password":"bad`+"\n"+`secret"}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
}

func TestMCPDatabaseSetupSaveWritesSQLiteConfig(t *testing.T) {
	dir := t.TempDir()
	mcpPath := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(mcpPath, []byte(`{"mcpServers":{"mail":{"command":"/usr/local/bin/mail-mcp"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(Deps{RuntimeConfig: &config.Config{
		MCPServersPath:       mcpPath,
		RuntimeWorkspacePath: "/workspace",
	}})

	rr := postRaw(t, router, "/mcp/setup/database", `{
		"provider":"sqlite",
		"sqlite_path":"/workspace/data/customer.db"
	}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var resp DatabaseSetupResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK || !resp.Status.Configured || resp.Status.Provider != "sqlite" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	servers, err := mcp.LoadServers(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := servers["mail"]; !ok {
		t.Fatalf("existing mail MCP server was not preserved: %+v", servers)
	}
	database := servers["database"]
	if database.Command != "/usr/local/bin/ea-database-server" {
		t.Fatalf("database command = %q", database.Command)
	}
	if len(database.Args) != 1 || database.Args[0] != "/workspace/data/customer.db" {
		t.Fatalf("database args = %+v", database.Args)
	}
}

func TestMCPDatabaseSetupSaveWritesPostgresAndKeepsPassword(t *testing.T) {
	dir := t.TempDir()
	mcpPath := filepath.Join(dir, "mcp.json")
	initial := `{"mcpServers":{"database":{"command":"/usr/local/bin/ea-database-server","args":["--postgresql","--host","old","--database","app","--user","reader","--password","secret"]}}}`
	if err := os.WriteFile(mcpPath, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(Deps{RuntimeConfig: &config.Config{
		MCPServersPath:       mcpPath,
		RuntimeWorkspacePath: "/workspace",
	}})

	rr := postRaw(t, router, "/mcp/setup/database", `{
		"provider":"postgresql",
		"host":"postgres",
		"port":5432,
		"database":"aura",
		"user":"reader",
		"ssl":true
	}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	if strings.Contains(rr.Body.String(), "secret") {
		t.Fatalf("setup response leaked password: %s", rr.Body.String())
	}
	servers, err := mcp.LoadServers(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	args := servers["database"].Args
	for _, want := range []string{"--postgresql", "--host", "postgres", "--database", "aura", "--user", "reader", "--password", "secret", "--ssl", "true"} {
		if !containsString(args, want) {
			t.Fatalf("args missing %q: %+v", want, args)
		}
	}
}

func TestMCPDatabaseSetupRejectsUnsafeFields(t *testing.T) {
	router := NewRouter(Deps{RuntimeConfig: &config.Config{
		MCPServersPath:       filepath.Join(t.TempDir(), "mcp.json"),
		RuntimeWorkspacePath: "/workspace",
	}})
	rr := postRaw(t, router, "/mcp/setup/database", `{"provider":"sqlite","sqlite_path":"/tmp/bad`+"\n"+`path.db"}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
}

func routerMCPClient(t *testing.T, name, url string) *mcp.Client {
	t.Helper()
	client, err := mcp.NewHTTPClient(name, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func strconvQuote(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}
