package manager

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
)

func TestRuntimeLaunchConfigEmptyCommand(t *testing.T) {
	_, err := RuntimeLaunchConfig("local", mcp.ManagedServer{
		Command: "   ",
		Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedLocal},
	})
	if err == nil || !strings.Contains(err.Error(), "command cannot be empty") {
		t.Fatalf("want empty-command error, got %v", err)
	}
}

func TestRuntimeKindDefaultsToLocal(t *testing.T) {
	cfg, err := RuntimeLaunchConfig("plain", mcp.ManagedServer{
		Command: "node",
		Source:  "recipe:plain",
	})
	if err != nil {
		t.Fatalf("RuntimeLaunchConfig: %v", err)
	}
	if cfg.Command != "node" {
		t.Fatalf("default runtime command = %q, want node", cfg.Command)
	}
}

func TestRuntimeKindHelper(t *testing.T) {
	if got := runtimeKind(mcp.ManagedServer{}); got != RuntimeLocal {
		t.Fatalf("runtimeKind(zero) = %q, want %q", got, RuntimeLocal)
	}
	if got := runtimeKind(mcp.ManagedServer{Runtime: mcp.ManagedRuntime{Kind: "custom"}}); got != "custom" {
		t.Fatalf("runtimeKind(custom) = %q, want custom", got)
	}
}

// A registry row can still declare a kind amendment #209 retired — read paths do not
// validate, by design — so the launcher must name it rather than fall through to the
// stdio branch and complain about an empty command.
func TestRuntimeLaunchConfigRejectsRetiredKinds(t *testing.T) {
	for _, kind := range []string{"docker", "docker_gateway", "  docker  "} {
		_, err := RuntimeLaunchConfig("legacy", mcp.ManagedServer{
			Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedLocal},
			Runtime: mcp.ManagedRuntime{Kind: kind},
		})
		if !errors.Is(err, errRetiredRuntimeKind) {
			t.Fatalf("RuntimeLaunchConfig(kind=%q) = %v, want errRetiredRuntimeKind", kind, err)
		}
	}
}

// normalizedTrustForServer delegates to mcp.Classify, so it inherits the transport-derived
// default. These cases used to assert TrustBlocked for everything unset — the F-013
// behaviour, removed because it could only ever stop a caller who had already been
// authorized, at the price of leaving an installed server inert with no on-screen way to
// finish it. Deliberately rewritten per CLAUDE.md: the assertions described the behaviour
// being changed, not a defect in the test.
func TestNormalizedTrustForServer(t *testing.T) {
	tests := []struct {
		name   string
		server mcp.ManagedServer
		want   string
	}{
		{name: "explicit class", server: mcp.ManagedServer{Trust: mcp.ManagedTrust{Class: mcp.TrustSandboxedLocal}}, want: mcp.TrustSandboxedLocal},
		{name: "explicitly blocked stays blocked", server: mcp.ManagedServer{Command: "node", Trust: mcp.ManagedTrust{Class: mcp.TrustBlocked}}, want: mcp.TrustBlocked},
		{name: "unknown explicit class falls back to the transport", server: mcp.ManagedServer{Command: "node", Source: "manual", Trust: mcp.ManagedTrust{Class: "admin"}}, want: mcp.TrustTrustedLocal},
		{name: "recipe source", server: mcp.ManagedServer{Source: "recipe:calculator"}, want: mcp.TrustTrustedRecipe},
		{name: "http type with no explicit trust", server: mcp.ManagedServer{Type: mcp.ServerTypeStreamableHTTP}, want: mcp.TrustRemoteHTTP},
		{name: "url implies http with no explicit trust", server: mcp.ManagedServer{URL: "https://mcp.example.com"}, want: mcp.TrustRemoteHTTP},
		{name: "bare stdio with no explicit trust", server: mcp.ManagedServer{Command: "node", Source: "manual"}, want: mcp.TrustTrustedLocal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizedTrustForServer(tt.server); got != tt.want {
				t.Fatalf("normalizedTrustForServer = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsStreamableHTTPServer(t *testing.T) {
	tests := []struct {
		name   string
		server mcp.ManagedServer
		want   bool
	}{
		{name: "type http", server: mcp.ManagedServer{Type: mcp.ServerTypeStreamableHTTP}, want: true},
		{name: "url set", server: mcp.ManagedServer{URL: "https://mcp.example.com"}, want: true},
		{name: "stdio", server: mcp.ManagedServer{Command: "node"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isStreamableHTTPServer(tt.server); got != tt.want {
				t.Fatalf("isStreamableHTTPServer = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunnableManagedServers(t *testing.T) {
	disabled := false
	doc := mcp.ManagedConfig{
		Profiles: map[string]mcp.ManagedProfile{
			"default": {Servers: []string{"calc", "blocked", "off", "remote"}},
		},
		ActiveProfile: "default",
		MCPServers: map[string]mcp.ManagedServer{
			"calc":    {Command: "uvx", Source: "recipe:calculator", Trust: mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe}},
			"blocked": {Command: "node", Source: "manual", Trust: mcp.ManagedTrust{Class: mcp.TrustBlocked}},
			"off":     {Command: "uvx", Source: "recipe:calculator", Enabled: &disabled},
			"remote":  {URL: "https://mcp.example.com", Type: mcp.ServerTypeStreamableHTTP, Trust: mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP}},
		},
	}

	got, err := RunnableManagedServers(doc)
	if err != nil {
		t.Fatalf("RunnableManagedServers: %v", err)
	}
	if _, ok := got["calc"]; !ok {
		t.Fatalf("calc should be runnable: %#v", got)
	}
	if _, ok := got["remote"]; !ok {
		t.Fatalf("remote http server should be runnable: %#v", got)
	}
	if _, ok := got["blocked"]; ok {
		t.Fatalf("blocked server should be skipped: %#v", got)
	}
	if _, ok := got["off"]; ok {
		t.Fatalf("disabled server should be skipped: %#v", got)
	}
}

func TestRunnableManagedServersHTTPEmptyURL(t *testing.T) {
	doc := mcp.ManagedConfig{
		Profiles:      map[string]mcp.ManagedProfile{"default": {Servers: []string{"remote"}}},
		ActiveProfile: "default",
		MCPServers: map[string]mcp.ManagedServer{
			"remote": {Type: mcp.ServerTypeStreamableHTTP, Trust: mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP}},
		},
	}
	if _, err := RunnableManagedServers(doc); err == nil || !strings.Contains(err.Error(), "url cannot be empty") {
		t.Fatalf("want url-empty error, got %v", err)
	}
}

// A bare-URL remote server with no explicit trust class is RUNNABLE. It used to be excluded
// (F-013), which is how a server the operator had just installed could sit in the list doing
// nothing. Blocking still works, but it has to be asked for — see the explicit case below.
func TestRunnableManagedServersBareRemoteIsRunnable(t *testing.T) {
	doc := mcp.ManagedConfig{
		Profiles:      map[string]mcp.ManagedProfile{"default": {Servers: []string{"remote"}}},
		ActiveProfile: "default",
		MCPServers: map[string]mcp.ManagedServer{
			"remote": {URL: "https://mcp.example.com"},
		},
	}
	got, err := RunnableManagedServers(doc)
	if err != nil {
		t.Fatalf("RunnableManagedServers: %v", err)
	}
	if _, ok := got["remote"]; !ok {
		t.Fatalf("bare-URL remote with no explicit trust should be runnable, got %#v", got)
	}
}

func TestRunnableManagedServersNoneRunnable(t *testing.T) {
	doc := mcp.ManagedConfig{
		Profiles:      map[string]mcp.ManagedProfile{"default": {Servers: []string{"blocked"}}},
		ActiveProfile: "default",
		MCPServers: map[string]mcp.ManagedServer{
			"blocked": {Command: "node", Source: "manual", Trust: mcp.ManagedTrust{Class: mcp.TrustBlocked}},
		},
	}
	got, err := RunnableManagedServers(doc)
	if err != nil {
		t.Fatalf("RunnableManagedServers: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil when none runnable, got %#v", got)
	}
}

func TestRuntimeServers(t *testing.T) {
	doc := mcp.ManagedConfig{
		Profiles: map[string]mcp.ManagedProfile{
			"default": {Servers: []string{"calc", "remote"}},
		},
		ActiveProfile: "default",
		MCPServers: map[string]mcp.ManagedServer{
			"calc":   {Command: "uvx", Args: []string{"calc"}, Source: "recipe:calculator", Trust: mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe}},
			"remote": {URL: "https://mcp.example.com", Type: mcp.ServerTypeStreamableHTTP, Trust: mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP}},
		},
	}

	got, err := RuntimeServers(doc)
	if err != nil {
		t.Fatalf("RuntimeServers: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("RuntimeServers should exclude http servers, got %#v", got)
	}
	cfg, ok := got["calc"]
	if !ok {
		t.Fatalf("calc launch config missing: %#v", got)
	}
	if cfg.Command != "uvx" || !reflect.DeepEqual(cfg.Args, []string{"calc"}) {
		t.Fatalf("calc launch config = %#v", cfg)
	}
}

func TestRuntimeServersNoneLaunchable(t *testing.T) {
	doc := mcp.ManagedConfig{
		Profiles:      map[string]mcp.ManagedProfile{"default": {Servers: []string{"remote"}}},
		ActiveProfile: "default",
		MCPServers: map[string]mcp.ManagedServer{
			"remote": {URL: "https://mcp.example.com", Type: mcp.ServerTypeStreamableHTTP, Trust: mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP}},
		},
	}
	got, err := RuntimeServers(doc)
	if err != nil {
		t.Fatalf("RuntimeServers: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil when only http servers, got %#v", got)
	}
}

func TestRuntimePolicyBlocksUntrustedLocal(t *testing.T) {
	_, err := RuntimeLaunchConfig("manual", mcp.ManagedServer{
		Command: "node",
		Source:  "manual",
		Trust:   mcp.ManagedTrust{Class: mcp.TrustBlocked},
	})
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("want blocked error, got %v", err)
	}
}
