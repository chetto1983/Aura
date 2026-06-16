// Spike 064 — Phase 0 interop gate: .NET MCP-over-HTTP (calendar-mcp Linux sidecar)
// <-> Aura's Go streamable-HTTP client. Proves initialize + tools/list + real
// tools/call + mcptools mount (namespaced calendar__*, Deferred) all interop.
//
// Run with the sidecar container up on 127.0.0.1:8093:
//
//	go run ./.planning/spikes/064-calendar-mcp-http-mount
//	AURA_MCP_CALENDAR_URL=http://127.0.0.1:8093/mcp/ go run ./.planning/spikes/064-calendar-mcp-http-mount
package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
)

func logf(cat, f string, a ...any) {
	fmt.Printf("%s [%s] %s\n", time.Now().UTC().Format(time.RFC3339), cat, fmt.Sprintf(f, a...))
}

func failf(f string, a ...any) {
	logf("ERROR", f, a...)
	os.Exit(1)
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	url := strings.TrimSpace(os.Getenv("AURA_MCP_CALENDAR_URL"))
	if url == "" {
		url = "http://127.0.0.1:8093/"
	}
	server := mcp.ManagedServer{
		Type:  mcp.ServerTypeStreamableHTTP,
		URL:   url,
		Trust: mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP},
	}
	logf("SETUP", "calendar sidecar (.NET HTTP) url=%s", url)

	cli, err := mcp.OpenServer(ctx, "calendar", server)
	if err != nil {
		failf("OpenServer streamable-http (.NET): %v", err)
	}
	defer func() { _ = cli.Close() }()
	logf("MCP", "initialize OK (.NET MCP over HTTP <-> Aura Go client)")

	if err := cli.Ping(ctx); err != nil {
		logf("WARN", "ping: %v (non-fatal)", err)
	} else {
		logf("MCP", "ping OK")
	}

	defs, err := cli.ListTools(ctx)
	if err != nil {
		failf("tools/list: %v", err)
	}
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		names = append(names, d.Name)
	}
	sort.Strings(names)
	logf("MCP", "tools/list=%d: %s", len(names), strings.Join(names, ", "))

	have := make(map[string]struct{}, len(names))
	for _, n := range names {
		have[n] = struct{}{}
	}
	for _, want := range []string{"list_accounts", "get_calendar_events", "create_event", "send_email"} {
		if _, ok := have[want]; !ok {
			failf("scaffold-expected tool %q not advertised", want)
		}
	}
	logf("MCP", "scaffold-expected tools present")

	// Ground truth: real tools/call over the .NET HTTP transport.
	acc, err := cli.CallTool(ctx, "list_accounts", nil)
	if err != nil {
		failf("CallTool list_accounts: %v", err)
	}
	logf("CALL", "list_accounts -> %s", oneLine(acc, 200))

	ev, err := cli.CallTool(ctx, "get_calendar_events", map[string]any{"timeZone": "Europe/Rome"})
	if err != nil {
		logf("WARN", "CallTool get_calendar_events: %v (zero-account env expected)", err)
	} else {
		logf("CALL", "get_calendar_events -> %s", oneLine(ev, 200))
	}

	// Aura mount path: namespacing + Deferred.
	reg := tools.NewRegistry()
	closer, mounted, err := mcptools.MountManagedServer(ctx, reg, "calendar", server)
	if err != nil {
		failf("mount: %v", err)
	}
	defer func() { _ = closer() }()
	sort.Strings(mounted)

	allDeferred := true
	for _, n := range mounted {
		if !strings.HasPrefix(n, "calendar__") {
			failf("mounted tool %q not namespaced calendar__*", n)
		}
		t, ok := reg.Get(n)
		if !ok {
			failf("mounted tool %q missing from registry", n)
		}
		if !t.Spec().Deferred {
			allDeferred = false
		}
	}
	logf("MOUNT", "mounted=%d namespaced calendar__* allDeferred=%v", len(mounted), allDeferred)
	if !allDeferred {
		failf("mounted MCP tools must be Deferred")
	}
	if len(mounted) != len(names) {
		failf("mounted %d != advertised %d (no DenyRisk filter applied here)", len(mounted), len(names))
	}

	logf("SUMMARY", "VALIDATED .NET MCP-over-HTTP <-> Aura Go client: initialize + %d tools listed + real tools/call + mount (calendar__*, Deferred) all interop", len(mounted))
}

func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}
