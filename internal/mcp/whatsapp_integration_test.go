//go:build whatsapp_integration

// Live WhatsApp bridge sidecar tier (D-110's "prove the real wire" tier), re-pointed
// at the SDK stdio session. No-skip-as-green (CLAUDE.md): when
// AURA_MCP_WHATSAPP_SERVER_JSON is unset under $CI the test t.Fatals (a skipped tier
// fails the gate, never passes it); locally it t.Skips.
package mcp

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// whatsappConfigOrGate resolves the stdio ServerConfig from
// AURA_MCP_WHATSAPP_SERVER_JSON. Empty under $CI is a HARD failure
// (no-skip-as-green); empty locally is a skip.
func whatsappConfigOrGate(t *testing.T) ServerConfig {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv("AURA_MCP_WHATSAPP_SERVER_JSON"))
	if raw == "" {
		if strings.TrimSpace(os.Getenv("CI")) != "" {
			t.Fatal("AURA_MCP_WHATSAPP_SERVER_JSON must be set under CI — a skipped whatsapp_integration tier is never a silent pass (CLAUDE.md no-skip-as-green)")
		}
		t.Skip("set AURA_MCP_WHATSAPP_SERVER_JSON to a ServerConfig JSON object + bring the whatsapp-mcp sidecar up to run the whatsapp_integration tier")
	}
	var cfg ServerConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("parse AURA_MCP_WHATSAPP_SERVER_JSON: %v", err)
	}
	return cfg
}

func TestWhatsAppServerLive(t *testing.T) {
	cfg := whatsappConfigOrGate(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := OpenSDKSessionForConfig(ctx, ctx, "whatsapp", cfg, SessionOptions{})
	if err != nil {
		t.Fatalf("OpenSDKSessionForConfig WhatsApp MCP server: %v", err)
	}
	defer func() { _ = session.Close() }()

	// D-105/RESEARCH Assumption A1: aura-whatsapp-mcp is very likely a Python MCP
	// SDK, whose negotiated protocol version was never measured against the live
	// sidecar before this plan. Log it — this is where that question actually gets
	// answered for the stdio-fronted fork.
	if result := session.InitializeResult(); result == nil || strings.TrimSpace(result.ProtocolVersion) == "" {
		t.Error("whatsapp sidecar: InitializeResult().ProtocolVersion is empty")
	} else {
		t.Logf("whatsapp sidecar negotiated protocol version: %s", result.ProtocolVersion)
	}

	advertised := drainSDKToolsForTest(t, ctx, session)
	for _, name := range []string{"search_contacts", "list_messages", "list_chats", "send_message"} {
		if !hasWhatsAppTool(advertised, name) {
			t.Fatalf("WhatsApp server did not advertise %q: %#v", name, advertised)
		}
	}
}

func hasWhatsAppTool(advertised []*sdkmcp.Tool, name string) bool {
	for _, def := range advertised {
		if def.Name == name {
			return true
		}
	}
	return false
}
