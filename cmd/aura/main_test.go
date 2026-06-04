package main

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/mcp"
	"go.uber.org/goleak"
)

// TestMain runs the cmd/aura package tests under goleak so the chat REPL's
// per-turn streaming + two-stage Ctrl+C teardown is asserted leak-free (D-10/
// Req#3): a leaked stream goroutine or an un-cancelled signal notifier would trip
// here. goleak verifies AFTER all tests complete.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// TestBuildRegistryFailSoft proves the D-21 fail-soft boot: a single
// dead/misconfigured MCP server entry no longer aborts boot. buildRegistryWithMCP
// must WARN-and-drop the broken server and return a usable registry (err==nil) with
// the non-deferred built-ins still registered (Pitfall 6 — a boot where every MCP
// server is dropped still passes Registry.Validate via the built-ins).
func TestBuildRegistryFailSoft(t *testing.T) {
	cfg := &config.Config{
		MCPServers: map[string]mcp.ServerConfig{
			"broken": {Command: "aura-nonexistent-mcp-binary-xyz"},
		},
	}
	reg, closers, err := buildRegistryWithMCP(context.Background(), cfg)
	defer func() { _ = closeMCPServers(closers) }()
	if err != nil {
		t.Fatalf("a broken MCP server must NOT abort boot (D-21); got err=%v", err)
	}
	if reg == nil {
		t.Fatal("buildRegistryWithMCP must return a usable registry after dropping the broken server")
	}
	// The broken server contributed no closer (it never mounted).
	if len(closers) != 0 {
		t.Fatalf("a server that failed to mount must add no closer, got %d", len(closers))
	}
	// The non-deferred built-ins survive — the registry is actionable (Pitfall 6).
	if err := reg.Validate(); err != nil {
		t.Fatalf("registry must stay valid after dropping every MCP server: %v", err)
	}
	if _, ok := reg.Get("text_response"); !ok {
		t.Fatal("base built-in text_response must remain registered after fail-soft drop")
	}
}
