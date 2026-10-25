//go:build docker_integration

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/packs"
	"github.com/chetto1983/aura/internal/plugins"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// whereServer is a plugin's own MCP server, started as `./bin/server`: its one tool answers, and
// writes into PLUGIN_DATA, the plugin root, the data directory and the working directory it sees.
const whereServer = `#!/usr/bin/env python3
import json, os, sys

def reply(msg_id, result):
    sys.stdout.write(json.dumps({"jsonrpc": "2.0", "id": msg_id, "result": result}) + "\n")
    sys.stdout.flush()

for line in sys.stdin:
    msg = json.loads(line)
    msg_id = msg.get("id")
    if msg_id is None:
        continue
    method = msg.get("method")
    if method == "initialize":
        reply(msg_id, {"protocolVersion": msg["params"]["protocolVersion"], "capabilities": {"tools": {}},
                       "serverInfo": {"name": "where", "version": "1"}})
    elif method == "tools/list":
        reply(msg_id, {"tools": [{"name": "where", "description": "Say where the server runs.",
                                  "inputSchema": {"type": "object"}}]})
    elif method == "tools/call":
        seen = " ".join([os.environ["PLUGIN_ROOT"], os.environ["PLUGIN_DATA"], os.getcwd()])
        with open(os.path.join(os.environ["PLUGIN_DATA"], "out"), "w") as f:
            f.write(seen + "\n")
        reply(msg_id, {"content": [{"type": "text", "text": seen}]})
    else:
        sys.stdout.write(json.dumps({"jsonrpc": "2.0", "id": msg_id,
                                     "error": {"code": -32601, "message": "unknown method"}}) + "\n")
        sys.stdout.flush()
`

// listWhereServers prints the command line of every plugin server process in the box; the bracket
// keeps the scanning shell's own command line from matching.
const listWhereServers = `for f in /proc/[0-9]*/cmdline; do tr '\0' ' ' < "$f" 2>/dev/null; echo; done | grep '[/]plugins/where/bin/server' || true`

// TestPluginServerAnswersFromItsRootInTheBox is spec 2026-10-25-pack-install §5.7 on the real
// launch path: a kept plugin tree mirrored into the box, its `./` server staged as an install
// stages it, and an MCP session opened through the box launcher a mount uses. The server answers
// from /plugins/<name> with PLUGIN_ROOT and PLUGIN_DATA set, writes into PLUGIN_DATA, and a
// second Resolve while it runs leaves its directory in place, and it leaves the box with its
// session. That the data outlives a box re-creation is
// TestPluginProcessSeesItsRootAndKeepsItsData, in usersandbox.
func TestPluginServerAnswersFromItsRootInTheBox(t *testing.T) {
	egressITDockerdOrGate(t)
	image := strings.TrimSpace(os.Getenv("AURA_SANDBOX_IMAGE"))
	if image == "" {
		image = "aura-sandbox:latest"
	}
	tree := t.TempDir()
	server := filepath.Join(tree, "bin", "server")
	if err := os.MkdirAll(filepath.Dir(server), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(server, []byte(whereServer), 0o755); err != nil { // #nosec G306 -- the fixture is the plugin's executable
		t.Fatal(err)
	}
	ref := packs.Ref{Source: "o/r", Directory: "where"}
	store := plugins.Store{Root: t.TempDir()}
	if err := store.Claim(plugins.Record{Name: "where", Ref: ref.String(), Commit: strings.Repeat("a", 40)}, tree); err != nil {
		t.Fatalf("claim: %v", err)
	}

	cfg := &config.Config{
		Profile:       config.ProfileSingleUserHardened,
		AuthulaSecret: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		PluginsDir:    store.Root,
		Sandbox:       config.SandboxConfig{Image: image, CPULimit: 1, MemoryLimit: 1 << 30, PidsLimit: 256},
	}
	id := egressITIdentity(t)
	router := buildSandboxRouter(cfg, nil)
	ctx := identityctx.WithIdentityID(context.Background(), id)
	t.Cleanup(func() {
		if err := router.Destroy(context.Background(), id); err != nil {
			t.Error(err)
		}
	})

	staged, err := plugins.StageServer(packs.Server{Name: "where", Command: "./bin/server"}, "where", ref, mcp.ManagedTrust{})
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	session, err := mcp.OpenSDKSession(ctx, "where", staged, mcp.EgressPolicy{}, mcp.SessionOptions{Box: newSandboxMCPBox(router)})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	listed, err := session.ListTools(ctx, nil)
	if err != nil || len(listed.Tools) != 1 || listed.Tools[0].Name != "where" {
		t.Fatalf("tools/list = %+v, %v", listed, err)
	}

	data := plugins.DataDir("where", ref)
	want := "/plugins/where " + data + " /plugins/where"
	where := func() string {
		t.Helper()
		res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "where", Arguments: map[string]any{}})
		if err != nil || res.IsError || len(res.Content) != 1 {
			t.Fatalf("where: %+v, %v", res, err)
		}
		text, ok := res.Content[0].(*sdkmcp.TextContent)
		if !ok {
			t.Fatalf("where answered %T", res.Content[0])
		}
		return text.Text
	}
	if got := where(); got != want {
		t.Fatalf("the server saw %q, want %q", got, want)
	}

	h, err := router.Route(ctx)
	if err != nil {
		t.Fatalf("a second resolve while the server runs: %v", err)
	}
	if got := where(); got != want {
		t.Fatalf("after a second resolve the server saw %q, want %q", got, want)
	}
	res, err := router.Exec(ctx, h, usersandbox.ExecRequest{Command: "cat " + data + "/out"})
	if err != nil || strings.TrimSpace(string(res.Stdout)) != want {
		t.Fatalf("PLUGIN_DATA/out = %q (exit %d, %v), want %q", res.Stdout, res.ExitCode, err, want)
	}

	// The scan must see the live server first, or its empty answer below proves nothing.
	if res, err := router.Exec(ctx, h, usersandbox.ExecRequest{Command: listWhereServers}); err != nil || strings.TrimSpace(string(res.Stdout)) == "" {
		t.Fatalf("the process scan did not find the running server: %q %v", res.Stdout, err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(200 * time.Millisecond) {
		res, err := router.Exec(ctx, h, usersandbox.ExecRequest{Command: listWhereServers})
		if err == nil && res.ExitCode == 0 && strings.TrimSpace(string(res.Stdout)) == "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the plugin server outlived its session: %s", res.Stdout)
		}
	}
}
