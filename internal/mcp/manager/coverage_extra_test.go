package manager

import (
	"reflect"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
)

func TestRuntimeServersProfileFilteringAndHTTPSkip(t *testing.T) {
	disabled := false
	doc := mcp.ManagedConfig{
		ActiveProfile: "default",
		Profiles: map[string]mcp.ManagedProfile{
			"default": {Servers: []string{"calc", "remote", "blocked", "off"}},
		},
		MCPServers: map[string]mcp.ManagedServer{
			"calc":    {Command: "uvx", Args: []string{"calculator"}, Source: "recipe:calculator"},
			"remote":  {URL: "https://mcp.example.com", Type: mcp.ServerTypeStreamableHTTP, Trust: mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP}},
			"blocked": {Command: "node", Trust: mcp.ManagedTrust{Class: mcp.TrustBlocked}},
			"off":     {Command: "uvx", Source: "recipe:calculator", Enabled: &disabled},
		},
	}

	runnable, err := RunnableManagedServers(doc)
	if err != nil {
		t.Fatalf("RunnableManagedServers: %v", err)
	}
	for _, name := range []string{"calc", "remote"} {
		if _, ok := runnable[name]; !ok {
			t.Fatalf("%s missing from runnable map: %#v", name, runnable)
		}
	}
	for _, name := range []string{"blocked", "off"} {
		if _, ok := runnable[name]; ok {
			t.Fatalf("%s should not be runnable: %#v", name, runnable)
		}
	}

	launchable, err := RuntimeServers(doc)
	if err != nil {
		t.Fatalf("RuntimeServers: %v", err)
	}
	if len(launchable) != 1 {
		t.Fatalf("RuntimeServers should exclude streamable HTTP servers: %#v", launchable)
	}
	if cfg := launchable["calc"]; cfg.Command != "uvx" || !reflect.DeepEqual(cfg.Args, []string{"calculator"}) {
		t.Fatalf("calc launch config = %#v", cfg)
	}
}

func TestRuntimeErrorAndClassificationBranches(t *testing.T) {
	if got := runtimeKind(mcp.ManagedServer{}); got != RuntimeLocal {
		t.Fatalf("runtimeKind default = %q, want %q", got, RuntimeLocal)
	}
	if got := normalizedTrustForServer(mcp.ManagedServer{Source: "recipe:mail"}); got != mcp.TrustTrustedRecipe {
		t.Fatalf("recipe trust = %q", got)
	}
	// An unset trust class resolves from the transport — see runtime_test.go's
	// TestNormalizedTrustForServer header for why blocking-by-default was dropped.
	if got := normalizedTrustForServer(mcp.ManagedServer{URL: "https://mcp.example.com"}); got != mcp.TrustRemoteHTTP {
		t.Fatalf("url trust = %q, want %q", got, mcp.TrustRemoteHTTP)
	}
	if got := normalizedTrustForServer(mcp.ManagedServer{Command: "node"}); got != mcp.TrustTrustedLocal {
		t.Fatalf("bare stdio trust = %q, want %q", got, mcp.TrustTrustedLocal)
	}
	if !isStreamableHTTPServer(mcp.ManagedServer{Type: mcp.ServerTypeStreamableHTTP}) {
		t.Fatal("streamable HTTP type not detected")
	}
	if isStreamableHTTPServer(mcp.ManagedServer{Command: "node"}) {
		t.Fatal("stdio server detected as streamable HTTP")
	}

	if _, err := RuntimeLaunchConfig("local", mcp.ManagedServer{Command: " ", Trust: mcp.ManagedTrust{Class: mcp.TrustTrustedLocal}}); err == nil {
		t.Fatal("empty local command accepted")
	}
	if _, err := RuntimeLaunchConfig("docker", mcp.ManagedServer{Trust: mcp.ManagedTrust{Class: mcp.TrustSandboxedLocal}, Runtime: mcp.ManagedRuntime{Kind: RuntimeDocker}}); err == nil {
		t.Fatal("empty docker image accepted")
	}
	if _, err := RuntimeLaunchConfig("gateway", mcp.ManagedServer{Trust: mcp.ManagedTrust{Class: mcp.TrustTrustedLocal}, Runtime: mcp.ManagedRuntime{Kind: RuntimeDockerGateway}}); err == nil {
		t.Fatal("empty gateway profile accepted")
	}
	_, err := RunnableManagedServers(mcp.ManagedConfig{
		ActiveProfile: "default",
		Profiles:      map[string]mcp.ManagedProfile{"default": {Servers: []string{"remote"}}},
		MCPServers: map[string]mcp.ManagedServer{
			"remote": {Type: mcp.ServerTypeStreamableHTTP, Trust: mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "url cannot be empty") {
		t.Fatalf("empty URL error = %v", err)
	}
}

func TestStatusAuthRuntimeBranches(t *testing.T) {
	if got := runtimeName(mcp.ManagedServer{Runtime: mcp.ManagedRuntime{Kind: "  docker  "}}); got != "docker" {
		t.Fatalf("runtimeName kind = %q", got)
	}
	if got := runtimeName(mcp.ManagedServer{URL: "https://mcp.example.com"}); got != mcp.TrustRemoteHTTP {
		t.Fatalf("runtimeName http = %q", got)
	}
	if got := runtimeName(mcp.ManagedServer{}); got != "local" {
		t.Fatalf("runtimeName default = %q", got)
	}
	authCases := map[string]mcp.ManagedServer{
		AuthUnsupported: {},
		AuthOAuth:       {Env: []string{"GOOGLE_OAUTH_CLIENT=abc"}},
		AuthBearerToken: {Env: []string{"Authorization=Bearer real"}},
		AuthNotLoggedIn: {Env: []string{"API_TOKEN=${API_TOKEN}"}},
	}
	for want, server := range authCases {
		if got := authStatus(server); got != want {
			t.Fatalf("authStatus = %q, want %q for %#v", got, want, server)
		}
	}
}
