// Spike 001: mail-mcp-live-mount — first-ever live mount of the mcptools seam
// against a real third-party stdio MCP server (martinzarfl/mail-mcp).
//
// Validates (Given/When/Then): Given mail-mcp built at D:/tmp/mail-mcp and a
// "mail" entry in Aura's managed MCP config (~/.aura/mcp/servers.json) carrying
// SMTP/IMAP creds, when this harness opens the server, mounts it into a fresh
// Registry, sends a uniquely-tagged email to self, and polls search_emails for
// the tag, then the namespaced mail__* tools register and the sent message is
// read back via IMAP — the exact ground-truth loop Phase 9's live E2E will use.
//
// Run: go run ./.planning/spikes/001-mail-mcp-live-mount
// Forensic log: every step prints an ISO-timestamped line; summary + exit code
// at the end (0 = VALIDATED, 1 = failure).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
)

func logf(cat, format string, a ...any) {
	fmt.Printf("%s [%s] %s\n", time.Now().UTC().Format(time.RFC3339), cat, fmt.Sprintf(format, a...))
}

func fail(format string, a ...any) {
	logf("FAIL", format, a...)
	os.Exit(1)
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// 1. Load the managed config (the same path bootChat's buildRegistryWithMCP uses).
	path, err := mcp.ManagedConfigPath()
	if err != nil {
		fail("managed config path: %v", err)
	}
	doc, err := mcp.LoadManagedConfig(path)
	if err != nil {
		fail("load managed config %s: %v", path, err)
	}
	srv, ok := doc.MCPServers["mail"]
	if !ok {
		fail("no %q server in %s — run: go run ./cmd/aura mcp add mail --env SMTP_HOST=... -- node D:/tmp/mail-mcp/dist/index.js", "mail", path)
	}
	logf("CONFIG", "found mail server: command=%s args=%v env_keys=%d", srv.Command, srv.Args, len(srv.Env))

	// Derive the self-address for the round-trip from the configured SMTP_USER.
	self := ""
	for _, kv := range srv.Env {
		if v, found := strings.CutPrefix(kv, "SMTP_USER="); found {
			self = v
		}
	}
	if self == "" {
		fail("mail server env has no SMTP_USER — cannot derive the send-to-self address")
	}
	logf("CONFIG", "send-to-self address: %s", self)

	// 2. Open the stdio server (spawn subprocess + initialize handshake).
	t0 := time.Now()
	cli, err := mcp.Open(ctx, "mail", mcp.ServerConfig{Command: srv.Command, Args: srv.Args, Env: srv.Env})
	if err != nil {
		fail("mcp.Open: %v", err)
	}
	defer func() { _ = cli.Close() }()
	logf("OPEN", "stdio handshake OK in %s", time.Since(t0).Round(time.Millisecond))

	// 3. ListTools — assert the tools Phase 9's allowlist (D-20) cares about.
	defs, err := cli.ListTools(ctx)
	if err != nil {
		fail("ListTools: %v", err)
	}
	names := make([]string, 0, len(defs))
	have := map[string]bool{}
	for _, d := range defs {
		names = append(names, d.Name)
		have[d.Name] = true
	}
	logf("TOOLS", "%d tools advertised: %s", len(defs), strings.Join(names, ", "))
	for _, want := range []string{"send_email", "fetch_emails", "search_emails", "get_thread"} {
		if !have[want] {
			fail("missing expected tool %q", want)
		}
	}
	// Footgun census for D-20 (allowlist rationale): tools v1 must NOT expose.
	for _, foot := range []string{"delete_mailbox", "move_message", "create_mailbox"} {
		if have[foot] {
			logf("FOOTGUN", "server also advertises %q — confirms D-20 allowlist need", foot)
		}
	}

	// 4. Mount into a fresh Registry — the 8.1 namespacing path the agent uses.
	reg := tools.NewRegistry()
	mounted, err := mcptools.Mount(ctx, reg, "mail", cli)
	if err != nil {
		fail("mcptools.Mount: %v", err)
	}
	logf("MOUNT", "%d tools mounted: %s", len(mounted), strings.Join(mounted, ", "))
	for _, n := range mounted {
		if !strings.HasPrefix(n, "mail__") {
			fail("mounted tool %q is not namespaced mail__*", n)
		}
		if len(n) > 64 {
			fail("mounted tool %q exceeds the 64-byte cap", n)
		}
	}
	manifest := reg.Render()
	logf("MANIFEST", "registry renders %d entries (alphabetical, deferred=%v for first)", len(manifest), manifest[0].Deferred)

	// 5. Round-trip: send a uniquely-tagged email to self via the RAW client...
	tag := fmt.Sprintf("AURA-SPIKE-001-%d", time.Now().Unix())
	sendOut, err := cli.CallTool(ctx, "send_email", map[string]any{
		"to":      self,
		"subject": tag,
		"text":    "Spike 001 ground-truth round-trip. Safe to delete.",
	})
	if err != nil {
		fail("send_email: %v", err)
	}
	logf("SEND", "send_email OK: %.120s", sendOut)

	// 6. ...then poll search_emails until the tag is visible via IMAP (<=90s).
	deadline := time.Now().Add(90 * time.Second)
	attempt := 0
	for {
		attempt++
		out, err := cli.CallTool(ctx, "search_emails", map[string]any{"query": tag})
		if err != nil {
			logf("POLL", "attempt %d: search_emails error: %v", attempt, err)
		} else if strings.Contains(out, tag) {
			logf("READBACK", "attempt %d: tag found via search_emails", attempt)
			break
		} else {
			logf("POLL", "attempt %d: tag not yet visible (%.80s...)", attempt, out)
		}
		if time.Now().After(deadline) {
			fail("read-back timed out after 90s (tag %s)", tag)
		}
		time.Sleep(5 * time.Second)
	}

	// 7. Exercise the BRIDGED Execute path too (what the agent actually calls):
	// fetch recent emails through the mounted mail__fetch_emails tool.
	bridged, ok := reg.Get("mail__search_emails")
	if !ok {
		fail("registry has no mail__search_emails after mount")
	}
	args, _ := json.Marshal(map[string]any{"query": tag})
	tctx := tools.WithToolCallContext(ctx, "spike-001", "call-1", os.TempDir(), 2048)
	res, err := bridged.Execute(tctx, args)
	if err != nil {
		fail("bridged Execute(mail__search_emails): %v", err)
	}
	if !strings.Contains(res.Preview, tag) && !strings.Contains(res.Preview, "truncated") {
		logf("WARN", "bridged preview does not show the tag (may be spilled): %.120s", res.Preview)
	}
	logf("BRIDGE", "bridged Execute OK: preview %d bytes, truncated=%v", len(res.Preview), res.Truncated)

	logf("SUMMARY", "VALIDATED — handshake, 8.1 namespacing, manifest, send-to-self, IMAP read-back, bridged Execute all green")
}
