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
	// The two-identity E2E's S3/garage-admin/Authula HTTP clients keep pooled idle
	// keep-alive connections (net/http.Transport persistConn read/write loops, created
	// by (*Transport).dialConn). goleak reports these as leaked at teardown even though
	// they are pooled and reaped on idle-timeout — the canonical http.Transport ignore.
	//
	// It MUST be IgnoreAnyFunction, not IgnoreTopFunction: a blocked readLoop's TOP
	// frame is internal/poll.runtime_pollWait (it parks in bufio.Peek→persistConn.Read),
	// so persistConn.readLoop is a MIDDLE frame — IgnoreTopFunction("...readLoop") never
	// matches. IgnoreAnyFunction matches the frame anywhere in the stack.
	goleak.VerifyTestMain(m,
		goleak.IgnoreAnyFunction("net/http.(*persistConn).readLoop"),
		goleak.IgnoreAnyFunction("net/http.(*persistConn).writeLoop"),
	)
}

func TestBuildRegistryBlockedManagedServerNotLaunched(t *testing.T) {
	path := withTempMCPConfig(t)
	// Disable the default-on memory recipe (15-02 / D-08) so this test isolates the
	// blocked-server assertion: with memory off, the ONLY policy is the blocked
	// server, which must be filtered out (no closer). The memory default-on +
	// disable-respect path is covered by config.TestMemoryDefaultOn*.
	disabled := false
	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"blocked": {Command: "aura-nonexistent-mcp-binary-xyz", Source: "manual", Trust: mcp.ManagedTrust{Class: mcp.TrustBlocked}},
		"memory":  {Type: mcp.ServerTypeStreamableHTTP, URL: "http://127.0.0.1:8091/mcp/", Source: "recipe:memory", Enabled: &disabled, Trust: mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe}},
	}}
	if err := mcp.SaveManagedConfig(path, doc); err != nil {
		t.Fatalf("save config: %v", err)
	}
	cfg := config.LoadDB()
	if _, ok := cfg.MCPServers["blocked"]; ok {
		t.Fatal("blocked managed server must be filtered before chat boot")
	}
	if _, ok := cfg.MCPPolicies["memory"]; ok {
		t.Fatal("explicitly disabled memory must not reach policies")
	}
	reg, _, closers, err := buildRegistryWithMCP(context.Background(), cfg, nil, nil)
	defer func() { _ = closeMCPServers(closers) }()
	if err != nil {
		t.Fatalf("buildRegistryWithMCP: %v", err)
	}
	if reg == nil || len(closers) != 0 {
		t.Fatalf("blocked server should not mount: reg nil=%v closers=%d", reg == nil, len(closers))
	}
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
	reg, _, closers, err := buildRegistryWithMCP(context.Background(), cfg, nil, nil)
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

// TestBuildBaseRegistryValidatesWithSwarmSpawn proves the Pitfall 6 ordering: the
// Deferred:true swarm_spawn is registered by buildBaseRegistry AND reg.Validate()
// still returns nil — because the non-deferred built-ins (not swarm_spawn) satisfy
// the >=1-non-deferred guard. The check exercises Validate AFTER swarm_spawn is
// present (buildBaseRegistry calls Validate internally and would os.Exit on a
// failure), then re-asserts the tool is registered with Deferred==true so a future
// refactor that flips the flag or drops the registration trips this test.
func TestBuildBaseRegistryValidatesWithSwarmSpawn(t *testing.T) {
	cfg := &config.Config{MaxSwarmGoals: 8}
	reg := buildBaseRegistry(cfg, nil) // os.Exits if Validate fails — reaching here proves it passed

	if err := reg.Validate(); err != nil {
		t.Fatalf("registry must stay valid with the Deferred swarm_spawn registered: %v", err)
	}

	tool, ok := reg.Get("swarm_spawn")
	if !ok {
		t.Fatal("buildBaseRegistry must register swarm_spawn into the parent registry (D-08/D-10)")
	}
	if !tool.Spec().Deferred {
		t.Fatal("swarm_spawn must be Deferred:true so it does not satisfy the non-deferred guard (Pitfall 6)")
	}
}
