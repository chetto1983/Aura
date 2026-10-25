//go:build docker_integration

package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
)

// Browser discovery is lazy; the first call of EACH identity must still get the
// recipe's cold-start budget, not the shorter reconnect budget.
func TestBrowserLazyColdStartGetsItsInitTimeout(t *testing.T) {
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
	ids := []string{egressITIdentity(t), egressITIdentity(t)}
	ctxOf := func(id string) context.Context { return identityctx.WithIdentityID(context.Background(), id) }
	t.Cleanup(func() {
		for _, id := range ids {
			if err := router.Destroy(context.Background(), id); err != nil {
				t.Error(err)
			}
		}
	})

	recipe, ok := mcpmanager.LookupCatalog("browser")
	if !ok {
		t.Fatal("browser recipe missing")
	}
	reg := tools.NewRegistry()
	mountCtx, cancel := context.WithTimeout(ctxOf(ids[0]), 10*time.Second)
	defer cancel()
	box := delayedBrowserBox{sandboxMCPBox{router: router}}
	closer, names, _, err := mcptools.MountManagedServerWithOptions(context.Background(), mountCtx, reg, "slow", recipe.Server,
		mcptools.MountOptions{Box: box})
	if err != nil {
		t.Fatalf("mount of a 12 s cold start under a 10 s mount budget: %v", err)
	}
	t.Cleanup(func() { _ = closer() })

	var tool tools.Tool
	for _, name := range names {
		if name == "slow__agent_browser_tools_profiles" {
			tool, _ = reg.Get(name)
		}
	}
	if tool == nil {
		t.Fatalf("slow__agent_browser_tools_profiles not among %d mounted tools", len(names))
	}
	for _, id := range ids {
		ctx := tools.WithToolCallContext(ctxOf(id), "sess", "tc", t.TempDir(), 4096)
		start := time.Now()
		if res, err := tool.Execute(ctx, json.RawMessage(`{}`)); err != nil || res.Preview == "" {
			t.Fatalf("identity's first 12 s cold start: %+v, %v", res, err)
		}
		if took := time.Since(start); took < 12*time.Second {
			t.Fatalf("start took %s; fixture did not exercise the cold-start budget", took)
		}
	}
}

type delayedBrowserBox struct{ sandboxMCPBox }

func (b delayedBrowserBox) StartBrowserStdio(ctx context.Context, start mcp.BoxStart, stdin io.ReadCloser, stdout io.Writer) (mcp.BoxProcess, error) {
	timer := time.NewTimer(12 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return b.sandboxMCPBox.StartBrowserStdio(ctx, start, stdin, stdout)
	}
}
