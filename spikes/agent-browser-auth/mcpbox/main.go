// mcpbox measures agent-browser's MCP server running INSIDE an identity box, reached the only
// way Aura reaches a box: an exec. It uses the production DockerBackend (ExecStream with stdin)
// and the go-sdk client over IOTransport, and prints what a mount would depend on: handshake
// and list latency, the manifest, a real tool round trip, and what the client sees when the box
// is suspended under it.
//
//	go run ./spikes/agent-browser-auth/mcpbox
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/moby/moby/client"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

const identity = "spike-agent-browser-mcp"

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cli, err := client.New(client.FromEnv)
	must(err)
	key := make([]byte, 32)
	backend := usersandbox.NewDockerBackend(cli, envOr("BOX_IMAGE", "aura-sandbox:latest"),
		usersandbox.Resources{NanoCPUs: 2e9, MemoryBytes: 2 << 30, PidsLimit: 512},
		usersandbox.WithEgress(envOr("BOX_EGRESS_IMAGE", "aura-egress:latest")),
		usersandbox.WithBoxFiles(func(string) ([]usersandbox.BoxFile, error) {
			return []usersandbox.BoxFile{{Path: "/run/aura/agent-browser.key", Content: []byte(hex.EncodeToString(key)), Mode: 0o600}}, nil
		}))
	spec := usersandbox.SandboxSpec{IdentityID: identity, Egress: usersandbox.EgressPolicy{Floor: true}}
	h, err := backend.Resolve(ctx, spec)
	must(err)
	defer func() { _ = backend.Stop(context.Background(), h) }()

	session := connect(ctx, backend, h)
	call(ctx, session, "agent_browser_open", map[string]any{"session": "mcp", "restore": true,
		"url": "data:text/html,<title>t</title><h1>Hello</h1><button onclick=\"document.title='clicked'\">Go</button>"})
	snap := call(ctx, session, "agent_browser_snapshot", map[string]any{"session": "mcp", "restore": true, "interactive": true})
	ref := firstRef(snap)
	call(ctx, session, "agent_browser_click", map[string]any{"session": "mcp", "restore": true, "selector": ref})
	call(ctx, session, "agent_browser_get_title", map[string]any{"session": "mcp", "restore": true})
	// Twice: the first screenshot of a browser pays a one-off warm-up.
	call(ctx, session, "agent_browser_screenshot", map[string]any{"session": "mcp", "restore": true})
	call(ctx, session, "agent_browser_screenshot", map[string]any{"session": "mcp", "restore": true})

	// What a mount sees when Aura suspends the idle box under a live session.
	start := time.Now()
	must(backend.Suspend(ctx, h))
	werr := session.Wait()
	fmt.Printf("suspend: session ended after %s (Wait=%v)\n", time.Since(start).Round(time.Millisecond), werr)
	must(backend.Resume(ctx, h))
	session2 := connect(ctx, backend, h)
	call(ctx, session2, "agent_browser_get_title", map[string]any{"session": "mcp", "restore": true})
	call(ctx, session2, "agent_browser_close", map[string]any{"session": "mcp", "restore": true})
	_ = session2.Close()
}

// connect runs the server in the box and speaks MCP over the exec's stdin and stdout. The
// server's stderr goes to a file: ExecStream merges both streams, and a log line on stdout
// would break the JSON-RPC framing.
func connect(ctx context.Context, backend *usersandbox.DockerBackend, h usersandbox.BoxHandle) *sdkmcp.ClientSession {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	start := time.Now()
	job, err := backend.ExecStream(ctx, h, usersandbox.ExecRequest{
		Command: "exec agent-browser mcp 2>>/tmp/agent-browser-mcp.log", Dir: "/workspace",
	}, inR, outW)
	must(err)
	go func() { _, _ = job.Wait(); _ = outW.Close() }()
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "mcpbox", Version: "0"}, nil)
	session, err := client.Connect(ctx, &sdkmcp.IOTransport{Reader: outR, Writer: inW}, nil)
	must(err)
	hs := time.Since(start)
	start = time.Now()
	tools, err := session.ListTools(ctx, nil)
	must(err)
	raw, _ := json.Marshal(tools.Tools)
	fmt.Printf("connect: handshake %s, tools/list %s: %d tools, %d bytes, next=%q\n",
		hs.Round(time.Millisecond), time.Since(start).Round(time.Millisecond), len(tools.Tools), len(raw), tools.NextCursor)
	return session
}

func call(ctx context.Context, s *sdkmcp.ClientSession, name string, args map[string]any) string {
	start := time.Now()
	res, err := s.CallTool(ctx, &sdkmcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		fmt.Printf("%s: transport error after %s: %v\n", name, time.Since(start).Round(time.Millisecond), err)
		return ""
	}
	var text strings.Builder
	kinds := []string{}
	for _, c := range res.Content {
		switch v := c.(type) {
		case *sdkmcp.TextContent:
			text.WriteString(v.Text)
			kinds = append(kinds, "text")
		case *sdkmcp.ImageContent:
			kinds = append(kinds, fmt.Sprintf("image(%s,%dB)", v.MIMEType, len(v.Data)))
		default:
			kinds = append(kinds, fmt.Sprintf("%T", c))
		}
	}
	out := text.String()
	fmt.Printf("%s: %s isError=%v content=%v text=%q\n", name, time.Since(start).Round(time.Millisecond), res.IsError, kinds, clip(out, 160))
	return out
}

func firstRef(snapshot string) string {
	for f := range strings.FieldsSeq(snapshot) {
		if ref, ok := strings.CutPrefix(f, "[ref="); ok {
			return "@" + strings.TrimSuffix(ref, "]")
		}
	}
	return "button"
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "mcpbox:", err)
		os.Exit(1)
	}
}
