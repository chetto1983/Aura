//go:build calculator_integration

package mcp

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCalculatorServerLive(t *testing.T) {
	raw := os.Getenv("AURA_MCP_CALCULATOR_SERVER_JSON")
	if strings.TrimSpace(raw) == "" {
		t.Skip("set AURA_MCP_CALCULATOR_SERVER_JSON to a mcp.ServerConfig JSON object")
	}
	var cfg ServerConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("parse AURA_MCP_CALCULATOR_SERVER_JSON: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := Open(ctx, "calculator", cfg)
	if err != nil {
		t.Fatalf("Open calculator MCP server: %v", err)
	}
	defer func() { _ = c.Close() }()

	defs, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if !hasTool(defs, "calculate") {
		t.Fatalf("calculator server did not advertise calculate: %#v", defs)
	}

	out, err := c.CallTool(ctx, "calculate", map[string]any{"expression": "2+2"})
	if err != nil {
		t.Fatalf("CallTool calculate: %v", err)
	}
	if !strings.Contains(out, "4") {
		t.Fatalf("calculate 2+2 returned %q, want content containing 4", out)
	}
}

func hasTool(defs []ToolDef, name string) bool {
	for _, def := range defs {
		if def.Name == name {
			return true
		}
	}
	return false
}
