//go:build memory_integration

// Live memory-MCP mount tier (port of spike 032). Proves Aura's bridge mounts the
// full agent-memory tool surface against the running, rebuilt sidecar: the mounted
// count equals the live tools/list count, every mounted spec is non-deferred, and
// every registered name is namespaced memory__*. NO DenyRisk filter is applied — Pitfall 2:
// the full surface is D-06, spike-035's mounted=13 blocked=3 was an exploration.
//
// No-skip-as-green (CLAUDE.md): when AURA_AGENT_MEMORY_MCP_URL is unset under $CI the
// test t.Fatals (a skipped tier fails the gate, never passes it); locally it t.Skips.
package mcptools

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
)

// reapIdleHTTPConns drains the shared http.DefaultClient's idle keep-alive
// connections at test end. The live streamable-HTTP MCP transport (mcp.OpenServer)
// uses http.DefaultClient, whose parked readLoop/writeLoop goroutines otherwise trip
// the package goleak TestMain even though Close() ended the MCP session. Reaping idle
// connections returns those goroutines synchronously; it is test-only and never
// touches production Close() semantics (which must not close a shared client).
func reapIdleHTTPConns(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		http.DefaultClient.CloseIdleConnections()
		// Give the netpoller a beat to unpark the reaped readLoop/writeLoop.
		time.Sleep(200 * time.Millisecond)
	})
}

func TestMemoryLiveScopedGraphToolsAcceptUserIdentifier(t *testing.T) {
	endpoint := memoryEndpointOrGate(t)
	reapIdleHTTPConns(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	cli, err := mcp.OpenServer(ctx, "memory-scoped-graph-probe", liveMemoryServer(endpoint))
	if err != nil {
		t.Fatalf("open streamable-http memory MCP at %s: %v", endpoint, err)
	}
	defer func() { _ = cli.Close() }()

	uid := fmt.Sprintf("codex-live-%d", time.Now().UnixNano())
	subject := "Codex Live Subject " + uid
	entityName := "Codex Live Place " + uid

	entityText, err := cli.CallTool(ctx, "memory_add_entity", map[string]any{
		"name":            entityName,
		"entity_type":     "LOCATION",
		"user_identifier": uid,
	})
	if err != nil {
		t.Fatalf("memory_add_entity: %v", err)
	}
	if strings.Contains(entityText, "disabled for user-scoped sessions") || !strings.Contains(entityText, `"stored": true`) {
		t.Fatalf("memory_add_entity returned unexpected result: %s", entityText)
	}

	factText, err := cli.CallTool(ctx, "memory_add_fact", map[string]any{
		"subject":         subject,
		"predicate":       "has_marker",
		"object_value":    "ok",
		"user_identifier": uid,
	})
	if err != nil {
		t.Fatalf("memory_add_fact: %v", err)
	}
	if strings.Contains(factText, "disabled for user-scoped sessions") || !strings.Contains(factText, `"stored": true`) {
		t.Fatalf("memory_add_fact returned unexpected result: %s", factText)
	}

	factsText, err := cli.CallTool(ctx, "memory_get_facts", map[string]any{
		"subject":         subject,
		"user_identifier": uid,
	})
	if err != nil {
		t.Fatalf("memory_get_facts: %v", err)
	}
	if strings.Contains(factsText, "disabled for user-scoped sessions") || !strings.Contains(factsText, `"fact_count": 1`) {
		t.Fatalf("memory_get_facts did not return the scoped fact: %s", factsText)
	}

	otherText, err := cli.CallTool(ctx, "memory_get_facts", map[string]any{
		"subject":         subject,
		"user_identifier": uid + "-other",
	})
	if err != nil {
		t.Fatalf("memory_get_facts other user: %v", err)
	}
	if !strings.Contains(otherText, `"fact_count": 0`) {
		t.Fatalf("memory_get_facts leaked scoped fact to another user: %s", otherText)
	}
}

// memoryEndpointOrGate resolves the live sidecar URL from AURA_AGENT_MEMORY_MCP_URL
// (or AURA_AGENT_MEMORY_MCP_PORT). Empty under $CI is a HARD failure (no-skip-as-green);
// empty locally is a skip.
func memoryEndpointOrGate(t *testing.T) string {
	t.Helper()
	if v := strings.TrimSpace(os.Getenv("AURA_AGENT_MEMORY_MCP_URL")); v != "" {
		return v
	}
	if port := strings.TrimSpace(os.Getenv("AURA_AGENT_MEMORY_MCP_PORT")); port != "" {
		return "http://127.0.0.1:" + port + "/mcp/"
	}
	if strings.TrimSpace(os.Getenv("CI")) != "" {
		t.Fatal("AURA_AGENT_MEMORY_MCP_URL (or _PORT) must be set under CI — a skipped memory_integration tier is never a silent pass (CLAUDE.md no-skip-as-green)")
	}
	t.Skip("set AURA_AGENT_MEMORY_MCP_URL (or AURA_AGENT_MEMORY_MCP_PORT) + bring the stack up to run the memory_integration tier")
	return ""
}

func liveMemoryServer(endpoint string) mcp.ManagedServer {
	return mcp.ManagedServer{
		Type:  mcp.ServerTypeStreamableHTTP,
		URL:   endpoint,
		Trust: mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
	}
}

// TestMemoryLiveMount mounts the running agent-memory sidecar through Aura's managed
// bridge and asserts: mounted count == live tools/list count, every mounted tool is
// non-deferred by default, and every registered name is memory__*. NO DenyRisk
// filter (full 16-tool surface, D-06/D-07).
func TestMemoryLiveMount(t *testing.T) {
	endpoint := memoryEndpointOrGate(t)
	reapIdleHTTPConns(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	server := liveMemoryServer(endpoint)

	// Ground truth: the live tools/list count (spike 032 saw 16).
	cli, err := mcp.OpenServer(ctx, "memory-probe", server)
	if err != nil {
		t.Fatalf("open streamable-http memory MCP at %s: %v", endpoint, err)
	}
	defs, err := cli.ListTools(ctx)
	if err != nil {
		_ = cli.Close()
		t.Fatalf("tools/list: %v", err)
	}
	_ = cli.Close()
	rawCount := len(defs)
	if rawCount == 0 {
		t.Fatalf("live sidecar advertised 0 tools — expected the agent-memory surface (16)")
	}

	reg := tools.NewRegistry()
	closer, mounted, err := MountManagedServer(ctx, reg, "memory", server)
	if err != nil {
		t.Fatalf("MountManagedServer: %v", err)
	}
	defer func() { _ = closer() }()

	if len(mounted) != rawCount {
		t.Fatalf("mounted %d tools, want %d (live tools/list count) — NO DenyRisk filter must drop nothing", len(mounted), rawCount)
	}

	for _, name := range mounted {
		if !strings.HasPrefix(name, "memory__") {
			t.Errorf("mounted tool %q is not namespaced memory__* (D-07)", name)
		}
		tl, ok := reg.Get(name)
		if !ok {
			t.Fatalf("mounted tool %q missing from registry", name)
		}
		if tl.Spec().Deferred {
			t.Errorf("mounted tool %q is Deferred; memory MCP tools must be in the default manifest", name)
		}
	}

	// Spot-check the recall tool the agent loop reaches via tool_search is present.
	if _, ok := reg.Get("memory__memory_search"); !ok {
		t.Errorf("memory__memory_search not registered — the spike-035 recall tool must mount")
	}
}
