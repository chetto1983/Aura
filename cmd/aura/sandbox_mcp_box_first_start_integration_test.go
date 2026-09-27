//go:build docker_integration

package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/client"

	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// TestBoxServerColdStartGetsItsInitTimeout mounts, the way `aura serve` does, a box server
// whose every start takes 12 s, as a cold `npx -y` or `uvx` fetch into an identity's empty
// caches can (10.95 s measured): longer than the 10 s default mount budget and the 10 s an
// identity's first call gets to redial. With the default init timeout it mounts in the first
// identity's box and answers the second identity from a process in that identity's own box.
func TestBoxServerColdStartGetsItsInitTimeout(t *testing.T) {
	egressITDockerdOrGate(t)
	image := strings.TrimSpace(os.Getenv("AURA_SANDBOX_IMAGE"))
	if image == "" {
		image = "aura-sandbox:latest"
	}
	cfg := &config.Config{
		Profile:       config.ProfileSingleUserHardened,
		AuthulaSecret: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Sandbox:       config.SandboxConfig{Image: image, CPULimit: 1, MemoryLimit: 1 << 30, PidsLimit: 256},
	}
	router := buildSandboxRouter(cfg, nil)
	cli, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	ids := []string{egressITIdentity(t), egressITIdentity(t)}
	ctxOf := func(id string) context.Context { return identityctx.WithIdentityID(context.Background(), id) }
	t.Cleanup(func() {
		for _, id := range ids {
			if h, err := router.Route(ctxOf(id)); err == nil {
				_ = usersandbox.NewDockerBackend(cli, image, usersandbox.Resources{}).Stop(context.Background(), h)
			}
		}
		_ = cli.Close()
	})

	server := mcp.ManagedServer{
		Command: "sh", Args: []string{"-c", "sleep 12; exec agent-browser mcp"},
		Runtime: mcp.ManagedRuntime{Kind: mcp.RuntimeKindBox},
	}
	reg := tools.NewRegistry()
	mountCtx, cancel := context.WithTimeout(ctxOf(ids[0]), 10*time.Second)
	defer cancel()
	start := time.Now()
	closer, names, _, err := mcptools.MountManagedServerWithOptions(context.Background(), mountCtx, reg, "slow", server,
		mcptools.MountOptions{Box: newSandboxMCPBox(router)})
	if err != nil {
		t.Fatalf("mount of a 12 s cold start under a 10 s mount budget: %v", err)
	}
	t.Cleanup(func() { _ = closer() })
	if took := time.Since(start); took < 12*time.Second {
		t.Fatalf("mount took %s, so the start was not cold and proves nothing", took)
	}

	var tool tools.Tool
	for _, name := range names {
		if name == "slow__agent_browser_tools_profiles" {
			tool, _ = reg.Get(name)
		}
	}
	if tool == nil {
		t.Fatalf("slow__agent_browser_tools_profiles not among %d mounted tools", len(names))
	}
	ctx := tools.WithToolCallContext(ctxOf(ids[1]), "sess", "tc", t.TempDir(), 4096)
	if res, err := tool.Execute(ctx, json.RawMessage(`{}`)); err != nil || res.Preview == "" {
		t.Fatalf("the second identity's first call, a 12 s cold start past the 10 s redial budget: %+v, %v", res, err)
	}
}
