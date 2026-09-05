package manager

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
)

// A trusted server declaring a retired runtime kind triggers a non-blocked error from
// RuntimeLaunchConfig, which RunnableManagedServers must propagate (rather than swallow
// as errMCPServerBlocked).
func TestRunnableManagedServersPropagatesNonBlockedError(t *testing.T) {
	doc := mcp.ManagedConfig{
		Profiles:      map[string]mcp.ManagedProfile{"default": {Servers: []string{"broken"}}},
		ActiveProfile: "default",
		MCPServers: map[string]mcp.ManagedServer{
			"broken": {
				Source:  "recipe:broken",
				Trust:   mcp.ManagedTrust{Class: mcp.TrustSandboxedLocal},
				Runtime: mcp.ManagedRuntime{Kind: "docker"},
			},
		},
	}
	_, err := RunnableManagedServers(doc)
	if err == nil || !strings.Contains(err.Error(), "runtime kind retired") {
		t.Fatalf("want retired-kind error, got %v", err)
	}
}

func TestRuntimeServersPropagatesError(t *testing.T) {
	doc := mcp.ManagedConfig{
		Profiles:      map[string]mcp.ManagedProfile{"default": {Servers: []string{"broken"}}},
		ActiveProfile: "default",
		MCPServers: map[string]mcp.ManagedServer{
			"broken": {
				Source:  "recipe:broken",
				Trust:   mcp.ManagedTrust{Class: mcp.TrustSandboxedLocal},
				Runtime: mcp.ManagedRuntime{Kind: "docker_gateway"},
			},
		},
	}
	_, err := RuntimeServers(doc)
	if err == nil || !strings.Contains(err.Error(), "runtime kind retired") {
		t.Fatalf("want retired-kind error, got %v", err)
	}
}

// A blocked server in the active profile is skipped by RunnableManagedServers,
// so a launch-time error never surfaces for it; pairing it with a runnable
// recipe server confirms the blocked one is filtered before RuntimeServers runs.
func TestRuntimeServersSkipsBlockedKeepsRunnable(t *testing.T) {
	doc := mcp.ManagedConfig{
		Profiles:      map[string]mcp.ManagedProfile{"default": {Servers: []string{"calc", "blocked"}}},
		ActiveProfile: "default",
		MCPServers: map[string]mcp.ManagedServer{
			"calc":    {Command: "uvx", Source: "recipe:calculator", Trust: mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe}},
			"blocked": {Command: "node", Source: "manual", Trust: mcp.ManagedTrust{Class: mcp.TrustBlocked}},
		},
	}
	got, err := RuntimeServers(doc)
	if err != nil {
		t.Fatalf("RuntimeServers: %v", err)
	}
	if _, ok := got["calc"]; !ok {
		t.Fatalf("calc should be launchable: %#v", got)
	}
	if _, ok := got["blocked"]; ok {
		t.Fatalf("blocked server must not be launchable: %#v", got)
	}
}
