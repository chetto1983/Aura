//go:build calendar_integration

package mcp

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCalendarServerLive(t *testing.T) {
	raw := os.Getenv("AURA_MCP_CALENDAR_SERVER_JSON")
	if strings.TrimSpace(raw) == "" {
		t.Skip("set AURA_MCP_CALENDAR_SERVER_JSON to a mcp.ServerConfig JSON object")
	}
	var cfg ServerConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("parse AURA_MCP_CALENDAR_SERVER_JSON: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := Open(ctx, "calendar", cfg)
	if err != nil {
		t.Fatalf("Open Calendar MCP server: %v", err)
	}
	defer func() { _ = c.Close() }()

	defs, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, name := range []string{"list_accounts", "get_calendar_events", "create_event", "send_email"} {
		if !hasCalendarTool(defs, name) {
			t.Fatalf("Calendar server did not advertise %q: %#v", name, defs)
		}
	}

	out, err := c.CallTool(ctx, "list_accounts", nil)
	if err != nil {
		t.Fatalf("CallTool list_accounts: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("list_accounts returned empty content")
	}
}

func hasCalendarTool(defs []ToolDef, name string) bool {
	for _, def := range defs {
		if def.Name == name {
			return true
		}
	}
	return false
}
