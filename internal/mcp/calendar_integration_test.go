//go:build calendar_integration

// Live Aura PIM sidecar tier (forked calendar-mcp → chetto1983/aura-pim-mcp). Drives
// the running streamable-HTTP sidecar end to end over the SDK session: connect +
// tools/list (exactly the trimmed surface, destructive tools dropped) + a real
// list_accounts call (clean even with zero connected accounts — the CI smoke shape).
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
// end. The SDK's streamable-HTTP transport falls back to http.DefaultClient when no
// EgressPolicy is enforced, whose parked readLoop/writeLoop goroutines otherwise trip
// the package goleak TestMain even after Close() ended the MCP session. Test-only;
// never touches production Close() semantics.
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

// TestCalendarServerLive drives the live Aura PIM sidecar over streamable-HTTP through
// the SDK session — the same OpenSDKSession construction path production uses.
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
	session, err := OpenSDKSession(ctx, "calendar", server, EgressPolicy{}, SessionOptions{})
	if err != nil {
		t.Fatalf("OpenSDKSession calendar PIM sidecar at %s: %v", endpoint, err)
	}
	defer func() { _ = session.Close() }()

	// D-105/RESEARCH Assumption A1: the fork is very likely a Python MCP SDK, whose
	// negotiated protocol version was never measured against the live sidecar before
	// this plan. Log it — this is where that question actually gets answered.
	result := session.InitializeResult()
	if result == nil || strings.TrimSpace(result.ProtocolVersion) == "" {
		t.Error("calendar sidecar: InitializeResult().ProtocolVersion is empty")
	} else {
		t.Logf("calendar sidecar negotiated protocol version: %s", result.ProtocolVersion)
	}

	advertised := drainSDKToolsForTest(t, ctx, session)
	got := make(map[string]bool, len(advertised))
	names := make([]string, 0, len(advertised))
	for _, d := range advertised {
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
	if len(advertised) != len(keptCalendarTools) {
		t.Errorf("PIM sidecar advertises %d tools, want exactly %d (trimmed surface); surface=%v", len(advertised), len(keptCalendarTools), names)
	}

	// list_accounts is read-only and clean even with zero connected accounts — the
	// CI smoke runs against an empty-config container.
	out, isErr := callAndDecodeForTest(t, ctx, session, "list_accounts", nil)
	if isErr {
		t.Fatalf("CallTool list_accounts reported isError: %s", out)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("list_accounts returned empty content")
	}
}
