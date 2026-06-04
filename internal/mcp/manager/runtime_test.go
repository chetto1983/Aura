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
