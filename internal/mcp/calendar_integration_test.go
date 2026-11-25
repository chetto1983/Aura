//go:build calendar_integration

// Live Aura PIM sidecar tier (forked calendar-mcp → chetto1983/aura-pim-mcp, one tool per
// operation since the 2026-11-23 split). Drives the running streamable-HTTP sidecar end to
// end over the SDK session: connect + tools/list (exactly the 29 operations, each with its
// own required list and no `action` argument) + real list_accounts calls for two identities
// on the same session (each must see only its own fixture account) + the MCP-05
// opaque-reference proof (SC#4).
//
// No-skip-as-green (CLAUDE.md): when AURA_PIM_MCP_URL (or _PORT) is unset under $CI the
// test t.Fatals (a skipped tier fails the gate, never passes it); locally it t.Skips.
package mcp

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

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

const (
	calendarTenantA        = "00000000-0000-0000-0000-000000000001"
	calendarTenantB        = "00000000-0000-0000-0000-000000000002"
	calendarTenantAccount  = "00000000000000000000000000000001__fixture"
	calendarForeignAccount = "00000000000000000000000000000002__fixture"
)

// pimToolRequired is the fork's advertised surface: each tool and its schema's root
// `required`, copied from the fork's own contract
// (src/CalendarMcp.Tests/Tools/SurfaceContractTests.cs at the pinned sha). The keys are
// the names Aura's stored approvals and drafts use; mcptools.CalendarToolNames() is the
// same set on Aura's side, but importing it here would cycle through this package.
var pimToolRequired = map[string][]string{
	"list_accounts":                {},
	"get_emails":                   {},
	"get_email_details":            {"accountId", "emailId"},
	"search_emails":                {"query"},
	"list_calendars":               {},
	"get_calendar_events":          {"timeZone"},
	"get_calendar_event_details":   {"timeZone", "calendarId", "eventId"},
	"get_contacts":                 {},
	"search_contacts":              {"query"},
	"get_contact_details":          {"accountId", "contactId"},
	"create_event":                 {"subject", "start", "end"},
	"update_event":                 {"calendarId", "eventId"},
	"respond_to_event":             {"eventId", "response"},
	"send_email":                   {"accountId", "to", "subject"},
	"delete_email":                 {"accountId", "emailId"},
	"mark_email_read":              {"accountId", "emailId", "isRead"},
	"move_email":                   {"accountId", "emailId", "destination"},
	"delete_event":                 {"eventId"},
	"create_contact":               {"displayName"},
	"update_contact":               {"accountId", "contactId"},
	"delete_contact":               {"accountId", "contactId"},
	"get_email_attachment":         {"accountId", "emailId", "attachmentId"},
	"get_contextual_email_summary": {},
	"get_guide":                    {},
	"get_unsubscribe_info":         {"accountId", "emailId"},
	"unsubscribe_from_email":       {"accountId", "emailId"},
	"bulk_delete_emails":           {"items"},
	"bulk_mark_emails_read":        {"items", "isRead"},
	"bulk_move_emails":             {"items", "destination"},
}

// TestCalendarServerLive drives the live Aura PIM sidecar over streamable-HTTP through
// the SDK session — the same OpenSDKSession construction path production uses.
func TestCalendarServerLive(t *testing.T) {
	endpoint := calendarEndpointOrGate(t)
	issuer := newOAuthResourceIssuer(t)
	tokenA := issuer.token(t, endpoint, calendarTenantA)
	tokenB := issuer.token(t, endpoint, calendarTenantB)
	reapIdleHTTPConns(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	assertOAuthResourceAuthenticates(t, ctx, issuer, endpoint, tokenA)

	sessionA := openOAuthResourceSession(t, ctx, issuer, "calendar-a", endpoint, tokenA)
	sessionB := openOAuthResourceSession(t, ctx, issuer, "calendar-b", endpoint, tokenB)
	assertOAuthResourceRejectsAnonymous(t, ctx, "calendar-anonymous", endpoint)

	result := sessionA.InitializeResult()
	if result == nil || strings.TrimSpace(result.ProtocolVersion) == "" {
		t.Error("calendar sidecar: InitializeResult().ProtocolVersion is empty")
	} else {
		t.Logf("calendar sidecar negotiated protocol version: %s", result.ProtocolVersion)
	}

	schemas := pimToolSchemas(t, drainSDKToolsForTest(t, ctx, sessionA))
	want := slices.Sorted(maps.Keys(pimToolRequired))
	if got := slices.Sorted(maps.Keys(schemas)); !slices.Equal(got, want) {
		t.Fatalf("PIM sidecar advertises %d tools %v, want the %d operations %v", len(got), got, len(want), want)
	}
	for name, schema := range schemas {
		if _, ok := schema.Properties["action"]; ok {
			t.Errorf("%s still takes an action argument", name)
		}
		if got, want := slices.Sorted(slices.Values(schema.Required)), slices.Sorted(slices.Values(pimToolRequired[name])); !slices.Equal(got, want) {
			t.Errorf("%s schema required = %v, want %v", name, got, want)
		}
	}

	// list_accounts is read-only. CI configures, as the operator, one local JSON
	// account per identity (testdata/pim-appsettings.json) so this tier proves
	// isolation, not merely empty-list behavior. The admin API cannot seed them: the
	// sidecar refuses a tenant-written JSON account that reads its own disk.
	_ = awaitCalendarAccount(t, ctx, sessionA, calendarTenantAccount, calendarForeignAccount)
	_ = awaitCalendarAccount(t, ctx, sessionB, calendarForeignAccount, calendarTenantAccount)

	assertOpaqueEventIDNeedsNoAccountID(t, ctx, sessionA)
}

func awaitCalendarAccount(
	t *testing.T,
	ctx context.Context,
	session *sdkmcp.ClientSession,
	want string,
	foreign string,
) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		out, isErr := callPIM(t, ctx, session, "list_accounts", map[string]any{})
		if isErr {
			t.Fatalf("CallTool list_accounts reported isError: %s", out)
		}
		if strings.Contains(out, foreign) {
			t.Fatalf("calendar account surface contains foreign tenant %q: %s", foreign, out)
		}
		if strings.Contains(out, want) {
			return out
		}
		if time.Now().After(deadline) {
			t.Fatalf("calendar account %q did not appear after configuration reload: %s", want, out)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for calendar account %q: %v", want, ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

type pimToolSchema struct {
	Properties map[string]json.RawMessage `json:"properties"`
	Required   []string                   `json:"required"`
}

// pimToolSchemas parses each advertised inputSchema — an `any` holding the CLIENT side's
// default JSON marshaling of the server's schema (a map[string]any, mirrors
// internal/agent/mcptools/bridge.go's schemaJSON) — keyed by tool name.
func pimToolSchemas(t *testing.T, tools []*sdkmcp.Tool) map[string]pimToolSchema {
	t.Helper()
	schemas := make(map[string]pimToolSchema, len(tools))
	for _, tool := range tools {
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal %s input schema: %v", tool.Name, err)
		}
		var schema pimToolSchema
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("unmarshal %s input schema: %v", tool.Name, err)
		}
		schemas[tool.Name] = schema
	}
	return schemas
}

var (
	eventIDInResponse    = regexp.MustCompile(`"eventId"\s*:\s*"([^"]+)"`)
	calendarIDInResponse = regexp.MustCompile(`"calendarId"\s*:\s*"([^"]+)"`)
)

// assertOpaqueEventIDNeedsNoAccountID is the MCP-05 / SC#4 proof. Its schema half is the
// surface check above: get_calendar_event_details' required list names no accountId. This
// round-trip half resolves an opaque eventId through to a detail call with no accountId
// argument. The live tier seeds a local JSON provider, so absence of an event is a failed
// fixture, never a green skip.
func assertOpaqueEventIDNeedsNoAccountID(t *testing.T, ctx context.Context, session *sdkmcp.ClientSession) {
	t.Helper()

	// Pin the query to the fixture's own 2026-08-24..25 window. The provider's
	// omitted-range default starts at the wall clock's current day, which made this
	// permanent fixture silently expire on 2026-08-26.
	out, isErr := callPIM(t, ctx, session, "get_calendar_events", map[string]any{
		"startDate": "2026-08-24T00:00:00Z",
		"endDate":   "2026-08-26T00:00:00Z",
		"timeZone":  "UTC",
	})
	if isErr {
		t.Fatalf("get_calendar_events reported isError: %s", out)
	}
	eventMatch := eventIDInResponse.FindStringSubmatch(out)
	calendarMatch := calendarIDInResponse.FindStringSubmatch(out)
	if len(eventMatch) < 2 || len(calendarMatch) < 2 {
		t.Fatalf("get_calendar_events returned no eventId/calendarId to chain from: %s", out)
	}

	detailOut, detailIsErr := callPIM(t, ctx, session, "get_calendar_event_details", map[string]any{
		"eventId":    eventMatch[1],
		"calendarId": calendarMatch[1],
		"timeZone":   "UTC",
	})
	if detailIsErr {
		t.Fatalf("get_calendar_event_details with the opaque eventId reference (no accountId argument) failed: %s", detailOut)
	}
	t.Logf("MCP-05 round-trip proven live: eventId reference resolved to event detail with no accountId argument dispatched")
}
