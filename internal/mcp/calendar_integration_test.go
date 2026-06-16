//go:build calendar_integration

// Live Aura PIM sidecar tier (forked calendar-mcp → chetto1983/aura-pim-mcp). Drives
// the running streamable-HTTP sidecar end to end: initialize + tools/list (exactly the
// trimmed surface, destructive tools dropped) + a real list_accounts call (clean even
// with zero connected accounts — the CI smoke shape).
//
// No-skip-as-green (CLAUDE.md): when AURA_PIM_MCP_URL (or _PORT) is unset under $CI the
// test t.Fatals (a skipped tier fails the gate, never passes it); locally it t.Skips.
package mcp

import (
	"context"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

// reapIdleHTTPConns drains http.DefaultClient's idle keep-alive connections at test
// end. OpenServer's streamable-HTTP transport uses http.DefaultClient, whose parked
// readLoop/writeLoop goroutines otherwise trip the package goleak TestMain even after
// Close() ended the MCP session. Test-only; never touches production Close() semantics.
func reapIdleHTTPConns(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		http.DefaultClient.CloseIdleConnections()
		time.Sleep(200 * time.Millisecond)
	})
}

// calendarEndpointOrGate resolves the live sidecar URL from AURA_PIM_MCP_URL (or
// AURA_PIM_MCP_PORT). Empty under $CI is a HARD failure (no-skip-as-green); empty
// locally is a skip.
func calendarEndpointOrGate(t *testing.T) string {
	t.Helper()
	if v := strings.TrimSpace(os.Getenv("AURA_PIM_MCP_URL")); v != "" {
		return v
	}
	if port := strings.TrimSpace(os.Getenv("AURA_PIM_MCP_PORT")); port != "" {
		return "http://127.0.0.1:" + port + "/"
	}
	if strings.TrimSpace(os.Getenv("CI")) != "" {
		t.Fatal("AURA_PIM_MCP_URL (or _PORT) must be set under CI — a skipped calendar_integration tier is never a silent pass (CLAUDE.md no-skip-as-green)")
	}
	t.Skip("set AURA_PIM_MCP_URL (or AURA_PIM_MCP_PORT) + bring the aura-pim-mcp sidecar up to run the calendar_integration tier")
	return ""
}

// keptCalendarTools is the trimmed PIM surface the fork registers (AURA-FORK.md): the
// 14 mail+calendar+contacts read/compose tools, with the destructive/bulk ones dropped
// server-side (defense in depth on top of Aura's Mutating-flagged permission layer).
var keptCalendarTools = []string{
	"list_accounts", "get_emails", "get_email_details", "search_emails", "send_email",
	"list_calendars", "get_calendar_events", "get_calendar_event_details", "create_event",
	"respond_to_event", "update_event", "get_contacts", "search_contacts", "get_contact_details",
}

// droppedCalendarTools must NOT be advertised — proves the fork's trim held.
var droppedCalendarTools = []string{
	"delete_email", "delete_event", "delete_contact", "bulk_delete_emails",
	"bulk_move_emails", "get_guide", "get_email_attachment",
}

// TestCalendarServerLive drives the live Aura PIM sidecar over streamable-HTTP.
func TestCalendarServerLive(t *testing.T) {
	endpoint := calendarEndpointOrGate(t)
	reapIdleHTTPConns(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	server := ManagedServer{
		Type:  ServerTypeStreamableHTTP,
		URL:   endpoint,
		Trust: ManagedTrust{Class: TrustTrustedRecipe},
	}
	c, err := OpenServer(ctx, "calendar", server)
	if err != nil {
		t.Fatalf("OpenServer calendar PIM sidecar at %s: %v", endpoint, err)
	}
	defer func() { _ = c.Close() }()

	defs, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	got := make(map[string]bool, len(defs))
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		got[d.Name] = true
		names = append(names, d.Name)
	}
	sort.Strings(names)

	for _, want := range keptCalendarTools {
		if !got[want] {
			t.Errorf("PIM sidecar did not advertise kept tool %q; surface=%v", want, names)
		}
	}
	for _, gone := range droppedCalendarTools {
		if got[gone] {
			t.Errorf("PIM sidecar still advertises dropped tool %q — fork trim regressed", gone)
		}
	}
	if len(defs) != len(keptCalendarTools) {
		t.Errorf("PIM sidecar advertises %d tools, want exactly %d (trimmed surface); surface=%v", len(defs), len(keptCalendarTools), names)
	}

	// list_accounts is read-only and clean even with zero connected accounts — the
	// CI smoke runs against an empty-config container.
	out, err := c.CallTool(ctx, "list_accounts", nil)
	if err != nil {
		t.Fatalf("CallTool list_accounts: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("list_accounts returned empty content")
	}
}
