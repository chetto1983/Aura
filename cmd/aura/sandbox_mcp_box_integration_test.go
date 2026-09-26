//go:build docker_integration

package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/client"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// listMCPServers prints the command line of every agent-browser MCP server in the box. The image
// has no pgrep; the bracket keeps the scanning shell's own command line from matching.
const listMCPServers = `for f in /proc/[0-9]*/cmdline; do tr '\0' ' ' < "$f" 2>/dev/null; echo; done | grep '[a]gent-browser mcp' || true`

// TestBrowserRecipeRunsItsMCPServerInTheBox is the box runtime end to end on the real image
// (prd.md §12): the catalog's browser recipe opens through the same SDK entry point a mount
// uses, agent-browser's MCP server starts in the identity's box behind the adapter, a real
// page is driven through it, and closing the session leaves no server process behind.
func TestBrowserRecipeRunsItsMCPServerInTheBox(t *testing.T) {
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
	id := egressITIdentity(t)
	router := buildSandboxRouter(cfg, nil)
	ctx := identityctx.WithIdentityID(context.Background(), id)
	h, err := router.Route(ctx)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	cli, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	t.Cleanup(func() {
		_ = usersandbox.NewDockerBackend(cli, image, usersandbox.Resources{}).Stop(context.Background(), h)
		_ = cli.Close()
	})

	recipe, ok := mcpmanager.LookupCatalog("browser")
	if !ok {
		t.Fatal("browser recipe missing from the catalog")
	}
	session, err := mcp.OpenSDKSession(ctx, "browser", recipe.Server, mcp.EgressPolicy{}, mcp.SessionOptions{Box: newSandboxMCPBox(router)})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	tools, err := session.ListTools(ctx, nil)
	if err != nil || len(tools.Tools) < 20 {
		t.Fatalf("tools/list: %d tools, %v", len(tools.Tools), err)
	}
	call := func(name string, args map[string]any) string {
		t.Helper()
		res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: name, Arguments: args})
		if err != nil || res.IsError {
			t.Fatalf("%s: %+v, %v", name, res, err)
		}
		var text strings.Builder
		for _, c := range res.Content {
			if tc, ok := c.(*sdkmcp.TextContent); ok {
				text.WriteString(tc.Text)
			}
		}
		return text.String()
	}
	call("agent_browser_open", map[string]any{"session": "mcpit", "url": "data:text/html,<title>t</title><button onclick=\"document.title='clicked'\">Go</button>"})
	call("agent_browser_click", map[string]any{"session": "mcpit", "selector": "button"})
	if title := call("agent_browser_get_title", map[string]any{"session": "mcpit"}); !strings.Contains(title, "clicked") {
		t.Fatalf("title after the click = %q", title)
	}
	call("agent_browser_close", map[string]any{"session": "mcpit"})

	// The scan must see the live server first, or its empty answer below proves nothing.
	if res, err := router.Exec(ctx, h, usersandbox.ExecRequest{Command: listMCPServers}); err != nil || strings.TrimSpace(string(res.Stdout)) == "" {
		t.Fatalf("the process scan did not find the running server: %q %v", res.Stdout, err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		res, err := router.Exec(ctx, h, usersandbox.ExecRequest{Command: listMCPServers})
		if err == nil && res.ExitCode == 0 && strings.TrimSpace(string(res.Stdout)) == "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("an agent-browser MCP server outlived its session: %s", res.Stdout)
		}
		time.Sleep(200 * time.Millisecond)
	}
	res, err := router.Exec(ctx, h, usersandbox.ExecRequest{Command: "test -f /tmp/aura-mcp-browser.log && echo logged"})
	if err != nil || strings.TrimSpace(string(res.Stdout)) != "logged" {
		t.Fatalf("the server's stderr did not go to its box log: %s %v", res.Stdout, err)
	}
}
