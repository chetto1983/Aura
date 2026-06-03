//go:build whatsapp_integration

package mcp

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestWhatsAppServerLive(t *testing.T) {
	raw := os.Getenv("AURA_MCP_WHATSAPP_SERVER_JSON")
	if strings.TrimSpace(raw) == "" {
		t.Skip("set AURA_MCP_WHATSAPP_SERVER_JSON to a mcp.ServerConfig JSON object")
	}
	var cfg ServerConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("parse AURA_MCP_WHATSAPP_SERVER_JSON: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := Open(ctx, "whatsapp", cfg)
	if err != nil {
		t.Fatalf("Open WhatsApp MCP server: %v", err)
	}
	defer func() { _ = c.Close() }()

	defs, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, name := range []string{"search_contacts", "list_messages", "list_chats", "send_message"} {
		if !hasWhatsAppTool(defs, name) {
			t.Fatalf("WhatsApp server did not advertise %q: %#v", name, defs)
		}
	}
}

func hasWhatsAppTool(defs []ToolDef, name string) bool {
	for _, def := range defs {
		if def.Name == name {
			return true
		}
	}
	return false
}
