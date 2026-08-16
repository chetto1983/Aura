//go:build calculator_integration

// Live calculator sidecar tier (D-110's "prove the real wire" tier), re-pointed at
// the SDK stdio session. No-skip-as-green (CLAUDE.md): when
// AURA_MCP_CALCULATOR_SERVER_JSON is unset under $CI the test t.Fatals (a skipped
// tier fails the gate, never passes it); locally it t.Skips.
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

// calculatorConfigOrGate resolves the stdio ServerConfig from
// AURA_MCP_CALCULATOR_SERVER_JSON. Empty under $CI is a HARD failure
// (no-skip-as-green); empty locally is a skip.
func calculatorConfigOrGate(t *testing.T) ServerConfig {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv("AURA_MCP_CALCULATOR_SERVER_JSON"))
	if raw == "" {
		if strings.TrimSpace(os.Getenv("CI")) != "" {
			t.Fatal("AURA_MCP_CALCULATOR_SERVER_JSON must be set under CI — a skipped calculator_integration tier is never a silent pass (CLAUDE.md no-skip-as-green)")
		}
		t.Skip("set AURA_MCP_CALCULATOR_SERVER_JSON to a ServerConfig JSON object to run the calculator_integration tier")
	}
	var cfg ServerConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("parse AURA_MCP_CALCULATOR_SERVER_JSON: %v", err)
	}
	return cfg
}

func TestCalculatorServerLive(t *testing.T) {
	cfg := calculatorConfigOrGate(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := OpenSDKSessionForConfig(ctx, ctx, "calculator", cfg, SessionOptions{})
	if err != nil {
		t.Fatalf("OpenSDKSessionForConfig calculator MCP server: %v", err)
	}
	defer func() { _ = session.Close() }()

	if result := session.InitializeResult(); result == nil || strings.TrimSpace(result.ProtocolVersion) == "" {
		t.Error("calculator sidecar: InitializeResult().ProtocolVersion is empty")
	} else {
		t.Logf("calculator sidecar negotiated protocol version: %s", result.ProtocolVersion)
	}

	advertised := drainSDKToolsForTest(t, ctx, session)
	if !hasCalculatorTool(advertised, "calculate") {
		t.Fatalf("calculator server did not advertise calculate: %#v", advertised)
	}

	out, isErr := callAndDecodeForTest(t, ctx, session, "calculate", map[string]any{"expression": "2+2"})
	if isErr {
		t.Fatalf("CallTool calculate reported isError: %s", out)
	}
	if !strings.Contains(out, "4") {
		t.Fatalf("calculate 2+2 returned %q, want content containing 4", out)
	}
}

func hasCalculatorTool(advertised []*sdkmcp.Tool, name string) bool {
	for _, def := range advertised {
		if def.Name == name {
			return true
		}
	}
	return false
}
