//go:build docker_integration

package main

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"sync"
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

// installedServer is a box server that exists only once its install line has run: the line
// writes a launcher for agent-browser's MCP server onto the identity's workspace, counts its
// own runs, and takes longer than the 10 s redial budget an identity's first call gets.
const installedServer = "/workspace/.mcp/abx/run"

const installLine = `mkdir -p /workspace/.mcp/abx
printf '#!/bin/sh\nexec agent-browser mcp\n' > /workspace/.mcp/abx/run
chmod +x /workspace/.mcp/abx/run
sleep 12
echo run >> /workspace/.mcp/abx/installs`

// TestBoxServerIsInstalledInEachIdentitysOwnBox mounts a box server that no box has yet, the
// way `aura serve` does, and calls it as two identities: each identity's box installs it once,
// on that identity's first call, however long the install takes; later calls reuse it; a
// changed install line runs again; and concurrent starts in one box install once.
func TestBoxServerIsInstalledInEachIdentitysOwnBox(t *testing.T) {
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
	installs := func(id string) int {
		t.Helper()
		ctx := ctxOf(id)
		h, err := router.Route(ctx)
		if err != nil {
			t.Fatalf("route: %v", err)
		}
		res, err := router.Exec(ctx, h, usersandbox.ExecRequest{Command: "cat /workspace/.mcp/abx/installs 2>/dev/null | wc -l"})
		if err != nil {
			t.Fatalf("count installs: %v", err)
		}
		n, err := strconv.Atoi(strings.TrimSpace(string(res.Stdout)))
		if err != nil {
			t.Fatalf("count installs: %q", res.Stdout)
		}
		return n
	}

	box := newSandboxMCPBox(router)
	server := mcp.ManagedServer{Command: installedServer, Runtime: mcp.ManagedRuntime{Kind: mcp.RuntimeKindBox, Install: installLine}}
	reg := tools.NewRegistry()
	mountCtx, cancel := context.WithTimeout(ctxOf(ids[0]), 10*time.Second)
	defer cancel()
	closer, names, _, err := mcptools.MountManagedServerWithOptions(context.Background(), mountCtx, reg, "abx", server, mcptools.MountOptions{Box: box})
	if err != nil {
		t.Fatalf("mount, installing in the first identity's box: %v", err)
	}
	t.Cleanup(func() { _ = closer() })
	tool := findTool(t, reg, names, "abx__agent_browser_tools_profiles")

	for _, id := range []string{ids[1], ids[0], ids[1]} {
		ctx := tools.WithToolCallContext(ctxOf(id), "sess", "tc", t.TempDir(), 4096)
		if res, err := tool.Execute(ctx, json.RawMessage(`{}`)); err != nil || res.Preview == "" {
			t.Fatalf("call as %s: %+v, %v", id, res, err)
		}
	}
	for _, id := range ids {
		if n := installs(id); n != 1 {
			t.Fatalf("identity %s installed %d times, want once in its own box", id, n)
		}
	}

	changed := installLine + "\n# v2"
	if err := box.Install(ctxOf(ids[0]), "abx", changed, nil); err != nil {
		t.Fatalf("changed install: %v", err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for range 3 {
		wg.Go(func() { errs <- box.Install(ctxOf(ids[1]), "abx", changed, nil) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent install: %v", err)
		}
	}
	if a, b := installs(ids[0]), installs(ids[1]); a != 2 || b != 2 {
		t.Fatalf("after a changed line and three concurrent starts: %d and %d installs, want exactly one more each", a, b)
	}

	// A failing install reports its exit code and the tail of its log, and records nothing, so
	// the next start tries again.
	err = box.Install(ctxOf(ids[0]), "broken", "echo no matching distribution >&2\nexit 3", nil)
	if err == nil || !strings.Contains(err.Error(), "exit 3") || !strings.Contains(err.Error(), "no matching distribution") {
		t.Fatalf("failed install = %v, want its exit code and the log tail", err)
	}
	h, _ := router.Route(ctxOf(ids[0]))
	res, err := router.Exec(ctxOf(ids[0]), h, usersandbox.ExecRequest{Command: "test -e /workspace/.aura-mcp/broken.installed && echo recorded || echo none"})
	if err != nil || strings.TrimSpace(string(res.Stdout)) != "none" {
		t.Fatalf("a failed install left a record: %q %v", res.Stdout, err)
	}
}

func findTool(t *testing.T, reg *tools.Registry, names []string, want string) tools.Tool {
	t.Helper()
	for _, name := range names {
		if name == want {
			tool, _ := reg.Get(name)
			return tool
		}
	}
	t.Fatalf("%s not mounted among %d tools", want, len(names))
	return nil
}
