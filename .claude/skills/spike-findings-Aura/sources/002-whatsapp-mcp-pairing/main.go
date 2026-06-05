// Spike 002: whatsapp-mcp-pairing — second live mount of the mcptools seam:
// lharries/whatsapp-mcp (Go whatsmeow bridge in WSL + Python/uv MCP server over
// `wsl` stdio). Validates the WhatsApp half of Phase 9's live E2E ground truth:
// send_message to SELF, then read the message back via list_messages.
//
// Pre-reqs (operator): bridge paired + running in WSL (REST :8080), "whatsapp"
// entry in ~/.aura/mcp/servers.json (source: spike-002), self number in
// AURA_SPIKE_WA_SELF or auto-extracted from the whatsmeow store.
//
// Run: go run ./.planning/spikes/002-whatsapp-mcp-pairing -self 39XXXXXXXXXX
package main

import (
	"context"
	"flag"
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
	self := flag.String("self", os.Getenv("AURA_SPIKE_WA_SELF"), "own phone number (digits, country code, no +)")
	readChat := flag.String("read-chat", "", "read-only mode: poll this chat JID for -expect instead of sending")
	expect := flag.String("expect", "", "substring to find in -read-chat mode")
	flag.Parse()
	if *self == "" && *readChat == "" {
		fail("pass -self 39XXXXXXXXXX (own WhatsApp number) or -read-chat <jid> -expect <text>")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	path, err := mcp.ManagedConfigPath()
	if err != nil {
		fail("managed config path: %v", err)
	}
	doc, err := mcp.LoadManagedConfig(path)
	if err != nil {
		fail("load managed config: %v", err)
	}
	srv, ok := doc.MCPServers["whatsapp"]
	if !ok {
		fail("no \"whatsapp\" server in %s", path)
	}
	logf("CONFIG", "whatsapp server: command=%s", srv.Command)

	// 1. Open: spawns `wsl ... uv run main.py` — stdio THROUGH wsl.exe is itself
	// part of what this spike validates (Windows host ↔ WSL-resident MCP server).
	t0 := time.Now()
	cli, err := mcp.Open(ctx, "whatsapp", mcp.ServerConfig{Command: srv.Command, Args: srv.Args, Env: srv.Env})
	if err != nil {
		fail("mcp.Open: %v", err)
	}
	defer func() { _ = cli.Close() }()
	logf("OPEN", "stdio handshake (via wsl.exe) OK in %s", time.Since(t0).Round(time.Millisecond))

	// 2. ListTools — assert the names the existing whatsapp_integration test expects.
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
	logf("TOOLS", "%d tools: %s", len(defs), strings.Join(names, ", "))
	for _, want := range []string{"search_contacts", "list_messages", "list_chats", "send_message"} {
		if !have[want] {
			fail("missing expected tool %q", want)
		}
	}

	// 3. Mount — 8.1 namespacing against a second real server.
	reg := tools.NewRegistry()
	mounted, err := mcptools.Mount(ctx, reg, "whatsapp", cli)
	if err != nil {
		fail("mcptools.Mount: %v", err)
	}
	for _, n := range mounted {
		if !strings.HasPrefix(n, "whatsapp__") || len(n) > 64 {
			fail("bad mounted name %q", n)
		}
	}
	logf("MOUNT", "%d tools mounted namespaced whatsapp__*", len(mounted))

	// Read-only mode (-read-chat): validate the READ half through the MCP tool —
	// list_messages on the given chat must surface -expect (a phone-sent message).
	if *readChat != "" {
		out, err := cli.CallTool(ctx, "list_messages", map[string]any{
			"chat_jid": *readChat,
			"limit":    10,
		})
		if err != nil {
			fail("list_messages(%s): %v", *readChat, err)
		}
		if !strings.Contains(out, *expect) {
			fail("expected %q not in list_messages output: %.300s", *expect, out)
		}
		logf("READBACK", "found %q in chat %s via MCP list_messages", *expect, *readChat)
		logf("SUMMARY", "READ HALF VALIDATED — wsl-stdio, namespacing, MCP read of phone-sent message all green")
		return
	}

	// 4. send_message to SELF with a unique tag.
	tag := fmt.Sprintf("AURA-SPIKE-002-%d", time.Now().Unix())
	sendOut, err := cli.CallTool(ctx, "send_message", map[string]any{
		"recipient": *self,
		"message":   "Spike 002 ground-truth round-trip: " + tag,
	})
	if err != nil {
		fail("send_message: %v", err)
	}
	logf("SEND", "send_message: %.160s", sendOut)
	if strings.Contains(strings.ToLower(sendOut), "error") || strings.Contains(sendOut, "false") {
		fail("send_message reported failure")
	}

	// 5. Read-back: poll list_messages until the tag appears in the self-chat
	// (the bridge stores its own sent messages in messages.db).
	deadline := time.Now().Add(60 * time.Second)
	attempt := 0
	for {
		attempt++
		out, err := cli.CallTool(ctx, "list_messages", map[string]any{
			"chat_jid": *self + "@s.whatsapp.net",
			"limit":    10,
		})
		if err != nil {
			logf("POLL", "attempt %d: list_messages error: %v", attempt, err)
		} else if strings.Contains(out, tag) {
			logf("READBACK", "attempt %d: tag found via list_messages", attempt)
			break
		} else {
			logf("POLL", "attempt %d: tag not yet visible (%.80s...)", attempt, strings.ReplaceAll(out, "\n", " "))
		}
		if time.Now().After(deadline) {
			fail("read-back timed out after 60s (tag %s)", tag)
		}
		time.Sleep(5 * time.Second)
	}

	logf("SUMMARY", "VALIDATED — wsl-stdio handshake, namespacing, send-to-self, store read-back all green")
}
