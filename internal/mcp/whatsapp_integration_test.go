//go:build whatsapp_integration

// Live Aura-owned WhatsApp remote-MCP tier. It drives one authenticated
// Streamable HTTP sessions for two OAuth subjects, proving both tenant isolation
// and fail-closed handling when the bearer is absent.
package mcp

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	whatsAppTenantA     = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	whatsAppTenantB     = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	whatsAppTenantOnlyA = "tenant-a-only"
	whatsAppTenantOnlyB = "tenant-b-only"
)

func whatsAppEndpointOrGate(t *testing.T) string {
	t.Helper()
	if endpoint := strings.TrimSpace(os.Getenv("AURA_WHATSAPP_MCP_URL")); endpoint != "" {
		return endpoint
	}
	if port := strings.TrimSpace(os.Getenv("AURA_WHATSAPP_MCP_PORT")); port != "" {
		return "http://127.0.0.1:" + port + "/mcp"
	}
	if strings.TrimSpace(os.Getenv("CI")) != "" {
		t.Fatal("AURA_WHATSAPP_MCP_URL (or _PORT) must be set under CI; this tier may not skip as green")
	}
	t.Skip("set AURA_WHATSAPP_MCP_URL and start the WhatsApp remote MCP to run this tier")
	return ""
}

func reapWhatsAppIdleHTTPConns(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		http.DefaultClient.CloseIdleConnections()
		time.Sleep(200 * time.Millisecond)
	})
}

func TestWhatsAppServerLiveTenantIsolation(t *testing.T) {
	endpoint := whatsAppEndpointOrGate(t)
	storeRoot := whatsAppStoreRootOrGate(t)
	issuer := newOAuthResourceIssuer(t)
	tokenA := issuer.token(t, endpoint, whatsAppTenantA)
	tokenB := issuer.token(t, endpoint, whatsAppTenantB)
	reapWhatsAppIdleHTTPConns(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	assertOAuthResourceAuthenticates(t, ctx, issuer, endpoint, tokenA)
	sessionA := openOAuthResourceSession(t, ctx, issuer, "whatsapp-a", endpoint, tokenA)
	sessionB := openOAuthResourceSession(t, ctx, issuer, "whatsapp-b", endpoint, tokenB)
	assertOAuthResourceRejectsAnonymous(t, ctx, "whatsapp-anonymous", endpoint)

	if result := sessionA.InitializeResult(); result == nil || strings.TrimSpace(result.ProtocolVersion) == "" {
		t.Error("WhatsApp MCP InitializeResult().ProtocolVersion is empty")
	}
	advertised := drainSDKToolsForTest(t, ctx, sessionA)
	for _, name := range []string{"search_contacts", "list_messages", "list_chats", "send_message", "get_media_data"} {
		if !hasWhatsAppTool(advertised, name) {
			t.Fatalf("WhatsApp MCP did not advertise %q", name)
		}
	}

	_ = callWhatsApp(t, ctx, sessionA)
	_ = callWhatsApp(t, ctx, sessionB)
	seedWhatsAppChat(t, ctx, storeRoot, whatsAppTenantA, whatsAppTenantOnlyA)
	seedWhatsAppChat(t, ctx, storeRoot, whatsAppTenantB, whatsAppTenantOnlyB)
	tenantA := callWhatsApp(t, ctx, sessionA)
	tenantB := callWhatsApp(t, ctx, sessionB)
	if !strings.Contains(tenantA, whatsAppTenantOnlyA) || strings.Contains(tenantA, whatsAppTenantOnlyB) {
		t.Fatalf("tenant A WhatsApp surface is not isolated: %s", tenantA)
	}
	if !strings.Contains(tenantB, whatsAppTenantOnlyB) || strings.Contains(tenantB, whatsAppTenantOnlyA) {
		t.Fatalf("tenant B WhatsApp surface is not isolated: %s", tenantB)
	}
}

func whatsAppStoreRootOrGate(t *testing.T) string {
	t.Helper()
	if root := strings.TrimSpace(os.Getenv("AURA_WHATSAPP_STORE_ROOT")); root != "" {
		return root
	}
	if strings.TrimSpace(os.Getenv("CI")) != "" {
		t.Fatal("AURA_WHATSAPP_STORE_ROOT must expose the live container store under CI")
	}
	t.Skip("set AURA_WHATSAPP_STORE_ROOT to the bind-mounted WhatsApp store")
	return ""
}

func seedWhatsAppChat(t *testing.T, ctx context.Context, root, identity, marker string) {
	t.Helper()
	path := filepath.Join(root, "tenants", identity, "store", "messages.db")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("tenant runtime database was not created: %s", path)
		}
		time.Sleep(50 * time.Millisecond)
	}
	db, err := sql.Open("sqlite3", "file:"+path+"?_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open tenant database %s: %v", identity, err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx,
		"INSERT OR REPLACE INTO chats (jid, name, last_message_time) VALUES (?, ?, ?)",
		marker+"@s.whatsapp.net", marker, nil); err != nil {
		t.Fatalf("seed tenant database %s: %v", identity, err)
	}
}

func callWhatsApp(t *testing.T, ctx context.Context, session *sdkmcp.ClientSession) string {
	t.Helper()
	result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "list_chats",
		Arguments: map[string]any{"include_last_message": false},
	})
	if err != nil {
		t.Fatalf("CallTool list_chats: %v", err)
	}
	text, isError := DecodeToolResult(result)
	if isError {
		t.Fatalf("CallTool list_chats returned isError: %s", text)
	}
	return text
}

func hasWhatsAppTool(advertised []*sdkmcp.Tool, name string) bool {
	for _, def := range advertised {
		if def.Name == name {
			return true
		}
	}
	return false
}
