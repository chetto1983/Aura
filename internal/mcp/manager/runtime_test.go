package manager

import (
	"reflect"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
)

func TestDockerRuntimeNoDefaultMounts(t *testing.T) {
	server := mcp.ManagedServer{
		Trust:   mcp.ManagedTrust{Class: mcp.TrustSandboxedLocal},
		Runtime: mcp.ManagedRuntime{Kind: RuntimeDocker, Image: "example/mcp:1", Command: []string{"server", "--stdio"}},
	}
	cfg, err := RuntimeLaunchConfig("third-party", server)
	if err != nil {
		t.Fatalf("RuntimeLaunchConfig: %v", err)
	}
	got := strings.Join(append([]string{cfg.Command}, cfg.Args...), " ")
	for _, want := range []string{"docker run -i --rm", "--network none", "example/mcp:1", "server --stdio"} {
		if !strings.Contains(got, want) {
			t.Fatalf("docker command missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "--mount") {
		t.Fatalf("docker command should not mount host paths by default: %s", got)
	}
}

func TestDockerRuntimeExplicitMountsAndLimits(t *testing.T) {
	server := mcp.ManagedServer{
		Env:   []string{"API_TOKEN=${API_TOKEN}"},
		Trust: mcp.ManagedTrust{Class: mcp.TrustSandboxedLocal},
		Runtime: mcp.ManagedRuntime{
			Kind:    RuntimeDocker,
			Image:   "example/mcp:1",
			Command: []string{"server"},
			Mounts:  []string{"type=bind,src=/safe,dst=/data,readonly"},
			Network: []string{"api.example.com"},
			CPUs:    "0.5",
			Memory:  "256m",
		},
	}
	cfg, err := RuntimeLaunchConfig("third-party", server)
	if err != nil {
		t.Fatalf("RuntimeLaunchConfig: %v", err)
	}
	got := strings.Join(append([]string{cfg.Command}, cfg.Args...), " ")
	for _, want := range []string{"--mount type=bind,src=/safe,dst=/data,readonly", "--cpus 0.5", "--memory 256m", "--network bridge"} {
		if !strings.Contains(got, want) {
			t.Fatalf("docker command missing %q: %s", want, got)
		}
	}
	if !reflect.DeepEqual(cfg.Env, []string{"API_TOKEN=${API_TOKEN}", "AURA_MCP_NETWORK_ALLOW=api.example.com"}) {
		t.Fatalf("env = %#v", cfg.Env)
	}
}

func TestDockerRuntimeKeepsWindowsMountsExplicit(t *testing.T) {
	mount := `type=bind,src=C:\Users\Davide\.aura,dst=/data,readonly`
	server := mcp.ManagedServer{
		Trust: mcp.ManagedTrust{Class: mcp.TrustSandboxedLocal},
		Runtime: mcp.ManagedRuntime{
			Kind:   RuntimeDocker,
			Image:  "example/mcp:1",
			Mounts: []string{mount},
		},
	}
	cfg, err := RuntimeLaunchConfig("windows-path", server)
	if err != nil {
		t.Fatalf("RuntimeLaunchConfig: %v", err)
	}
	for i, arg := range cfg.Args {
		if arg == "--mount" && i+1 < len(cfg.Args) && cfg.Args[i+1] == mount {
			return
		}
	}
	t.Fatalf("docker args did not preserve explicit Windows mount: %#v", cfg.Args)
}

func TestGatewayRuntime(t *testing.T) {
	server := mcp.ManagedServer{
		Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedLocal},
		Runtime: mcp.ManagedRuntime{Kind: RuntimeDockerGateway, Profile: "team"},
	}
	cfg, err := RuntimeLaunchConfig("gateway", server)
	if err != nil {
		t.Fatalf("RuntimeLaunchConfig: %v", err)
	}
	wantArgs := []string{"mcp", "gateway", "run", "--profile", "team"}
	if cfg.Command != "docker" || !reflect.DeepEqual(cfg.Args, wantArgs) {
		t.Fatalf("gateway config = %#v, want docker %#v", cfg, wantArgs)
	}
}

func TestRuntimeLaunchConfigEmptyCommand(t *testing.T) {
	_, err := RuntimeLaunchConfig("local", mcp.ManagedServer{
		Command: "   ",
		Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedLocal},
	})
	if err == nil || !strings.Contains(err.Error(), "command cannot be empty") {
		t.Fatalf("want empty-command error, got %v", err)
	}
}

func TestGatewayRuntimeEmptyProfile(t *testing.T) {
	_, err := RuntimeLaunchConfig("gateway", mcp.ManagedServer{
		Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedLocal},
		Runtime: mcp.ManagedRuntime{Kind: RuntimeDockerGateway, Profile: "  "},
	})
	if err == nil || !strings.Contains(err.Error(), "gateway profile cannot be empty") {
		t.Fatalf("want gateway-profile error, got %v", err)
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
	if got := runtimeKind(mcp.ManagedServer{Runtime: mcp.ManagedRuntime{Kind: RuntimeDocker}}); got != RuntimeDocker {
		t.Fatalf("runtimeKind(docker) = %q, want %q", got, RuntimeDocker)
	}
}

func TestNormalizedTrustForServer(t *testing.T) {
	tests := []struct {
		name   string
		server mcp.ManagedServer
		want   string
	}{
		{name: "explicit class", server: mcp.ManagedServer{Trust: mcp.ManagedTrust{Class: mcp.TrustSandboxedLocal}}, want: mcp.TrustSandboxedLocal},
		{name: "recipe source", server: mcp.ManagedServer{Source: "recipe:calculator"}, want: mcp.TrustTrustedRecipe},
		{name: "http type", server: mcp.ManagedServer{Type: mcp.ServerTypeStreamableHTTP}, want: mcp.TrustRemoteHTTP},
		{name: "url implies http", server: mcp.ManagedServer{URL: "https://mcp.example.com"}, want: mcp.TrustRemoteHTTP},
		{name: "unknown blocked", server: mcp.ManagedServer{Command: "node", Source: "manual"}, want: mcp.TrustBlocked},
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
