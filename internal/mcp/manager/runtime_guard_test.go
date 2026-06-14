package manager

import (
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
)

func TestRuntimeGuardBlocksDockerRuntimesInContainer(t *testing.T) {
	tests := []struct {
		name    string
		server  mcp.ManagedServer
		wantMsg string
	}{
		{
			name: "docker runtime",
			server: mcp.ManagedServer{
				Trust:   mcp.ManagedTrust{Class: mcp.TrustSandboxedLocal},
				Runtime: mcp.ManagedRuntime{Kind: RuntimeDocker, Image: "example/mcp:1"},
			},
			wantMsg: "deploy as a compose sibling",
		},
		{
			name: "docker gateway runtime",
			server: mcp.ManagedServer{
				Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedLocal},
				Runtime: mcp.ManagedRuntime{Kind: RuntimeDockerGateway, Profile: "team"},
			},
			wantMsg: "deploy as a compose sibling",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AURA_IN_CONTAINER", "1")

			cfg, err := RuntimeLaunchConfig("needs-container", tt.server)
			if err == nil {
				t.Fatalf("RuntimeLaunchConfig returned nil error with config %#v", cfg)
			}
			if !errors.Is(err, errDockerRuntimeInContainer) {
				t.Fatalf("error %v does not wrap errDockerRuntimeInContainer", err)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantMsg)
			}
		})
	}
}

func TestRuntimeGuardAllowsDockerRuntimeOffBox(t *testing.T) {
	t.Setenv("AURA_IN_CONTAINER", "")

	cfg, err := RuntimeLaunchConfig("host-docker", mcp.ManagedServer{
		Trust:   mcp.ManagedTrust{Class: mcp.TrustSandboxedLocal},
		Runtime: mcp.ManagedRuntime{Kind: RuntimeDocker, Image: "example/mcp:1", Command: []string{"serve"}},
	})
	if err != nil {
		t.Fatalf("RuntimeLaunchConfig: %v", err)
	}
	if cfg.Command != "docker" {
		t.Fatalf("cfg.Command = %q, want docker", cfg.Command)
	}
	got := strings.Join(cfg.Args, " ")
	if !strings.Contains(got, "example/mcp:1") || !strings.Contains(got, "serve") {
		t.Fatalf("docker args = %#v, want image and command", cfg.Args)
	}
}
