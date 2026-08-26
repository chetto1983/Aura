//go:build calendar_integration

// Live Aura PIM sidecar tier (forked calendar-mcp → chetto1983/aura-pim-mcp,
// curated onto ONE `calendar` action tool by 46-05/46-06). Drives the running
// streamable-HTTP sidecar end to end over the SDK session: connect + tools/list
// (exactly one curated tool, action enum matching the design doc) + real
// list_accounts calls for two identities on the same session (each must see only
// its own fixture account) + the MCP-05 opaque-reference proof (SC#4).
//
// No-skip-as-green (CLAUDE.md): when AURA_PIM_MCP_URL (or _PORT) is unset under $CI the
// test t.Fatals (a skipped tier fails the gate, never passes it); locally it t.Skips.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
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

const (
	calendarTenantA        = "00000000-0000-0000-0000-000000000001"
	calendarTenantB        = "00000000-0000-0000-0000-000000000002"
	calendarTenantAccount  = "00000000000000000000000000000001__fixture"
	calendarForeignAccount = "00000000000000000000000000000002__fixture"
)

// curatedCalendarToolName is the fork's own advertised wire name (measured live
// 2026-08-22 against ghcr.io/chetto1983/aura-pim-mcp:38c94fd9d...) — this test
// talks to the sidecar directly over the SDK session, the same handshake
// bridge.go's initial mount performs before namespacedName prefixes it
// "calendar__" for the model.
const curatedCalendarToolName = "calendar"

// calendarActionEnum is the design doc's §5a action list, in the fork's own
// tools/list order (docs/superpowers/specs/2026-08-17-mcp-curated-surface-design.md).
// The live curated tool's schema.properties.action.enum must equal this exactly.
var calendarActionEnum = []string{
	"list_accounts", "get_emails", "get_email_details", "search_emails",
	"list_calendars", "get_calendar_events", "get_calendar_event_details",
	"get_contacts", "search_contacts", "get_contact_details",
	"create_event", "update_event", "respond_to_event", "send_email",
	// Amended 2026-08-22: mail management restored. The provider layer exposes 21
	// operations and only 14 were ever registered as tools, so delete/mark-read/move
	// were implemented for every provider and reachable by none.
	"delete_email", "mark_email_read", "move_email",
	// Amended 2026-08-23: the twelve b01413620 tiered. That commit did three of the
	// four things the bump needed — it tiered all twelve in trustedRecipeActions and
	// moved compose.yaml's pin to the 29-action fork — but left THIS list and
	// ci.yml's AURA_PIM_MCP_IMAGE on the 17-action one. The two stragglers together
	// produced a false green: CI kept passing because it ran the old image against
	// this old list, while the deployment, which runs the pin compose names, failed
	// the tier. Measured 2026-08-23 against the running
	// ghcr.io/chetto1983/aura-pim-mcp:c497224cf8a0..., whose tools/list order this
	// continues.
	"delete_event", "create_contact", "update_contact", "delete_contact",
	"get_email_attachment", "get_contextual_email_summary",
	"get_guide", "get_unsubscribe_info", "unsubscribe_from_email",
	"bulk_delete_emails", "bulk_mark_emails_read", "bulk_move_emails",
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
	seedCalendarAccount(t, ctx, endpoint, tokenA, calendarTenantA)
	seedCalendarAccount(t, ctx, endpoint, tokenB, calendarTenantB)

	sessionA := openOAuthResourceSession(t, ctx, issuer, "calendar-a", endpoint, tokenA)
	sessionB := openOAuthResourceSession(t, ctx, issuer, "calendar-b", endpoint, tokenB)
	assertOAuthResourceRejectsAnonymous(t, ctx, "calendar-anonymous", endpoint)

	result := sessionA.InitializeResult()
	if result == nil || strings.TrimSpace(result.ProtocolVersion) == "" {
		t.Error("calendar sidecar: InitializeResult().ProtocolVersion is empty")
	} else {
		t.Logf("calendar sidecar negotiated protocol version: %s", result.ProtocolVersion)
	}

	advertised := drainSDKToolsForTest(t, ctx, sessionA)
	if len(advertised) != 1 {
		names := make([]string, 0, len(advertised))
		for _, d := range advertised {
			names = append(names, d.Name)
		}
		t.Fatalf("PIM sidecar advertises %d tools, want exactly 1 (curated calendar tool); surface=%v", len(advertised), names)
	}
	tool := advertised[0]
	if tool.Name != curatedCalendarToolName {
		t.Fatalf("PIM sidecar's curated tool is named %q, want %q", tool.Name, curatedCalendarToolName)
	}

	gotEnum, required := calendarActionSchema(t, tool)
	if !slices.Equal(gotEnum, calendarActionEnum) {
		t.Errorf("curated tool action enum = %v, want %v", gotEnum, calendarActionEnum)
	}
	if !slices.Equal(required, []string{"action"}) {
		t.Errorf("curated tool schema required = %v, want [action] — the flat-union D-19 shape has no other root-required field", required)
	}

	// list_accounts is read-only. CI seeds the same local JSON provider for two
	// identities so this tier proves isolation, not merely empty-list behavior.
	_ = awaitCalendarAccount(t, ctx, sessionA, calendarTenantAccount, calendarForeignAccount)
	_ = awaitCalendarAccount(t, ctx, sessionB, calendarForeignAccount, calendarTenantAccount)

	assertOpaqueEventIDNeedsNoAccountID(t, ctx, sessionA, tool)
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
		out, isErr := callCalendar(t, ctx, session, map[string]any{"action": "list_accounts"})
		if isErr {
			t.Fatalf("CallTool calendar(list_accounts) reported isError: %s", out)
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

func seedCalendarAccount(t *testing.T, ctx context.Context, endpoint, token, identity string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"id": "fixture", "displayName": "Fixture calendar (no provider OAuth)",
		"provider": "json", "enabled": true, "priority": 1,
		"providerConfig": map[string]string{"source": "local", "filePath": "/app/data/fixture-calendar.json"},
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(endpoint, "/")+"/admin/accounts", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("seed calendar account for %s: %v", identity, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusConflict {
		t.Fatalf("seed calendar account for %s: HTTP %d", identity, response.StatusCode)
	}
}

func callCalendar(t *testing.T, ctx context.Context, session *sdkmcp.ClientSession, args map[string]any) (text string, isError bool) {
	t.Helper()
	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: curatedCalendarToolName, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", curatedCalendarToolName, err)
	}
	return DecodeToolResult(res)
}

// calendarActionSchema parses the curated tool's advertised inputSchema — an `any`
// holding the CLIENT side's default JSON marshaling of the server's schema (a
// map[string]any, mirrors internal/agent/mcptools/bridge.go's schemaJSON) — into
// its action enum and root-level required list.
func calendarActionSchema(t *testing.T, tool *sdkmcp.Tool) (enum, required []string) {
	t.Helper()
	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("marshal curated tool input schema: %v", err)
	}
	var schema struct {
		Properties struct {
			Action struct {
				Enum []string `json:"enum"`
			} `json:"action"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal curated tool input schema: %v", err)
	}
	return schema.Properties.Action.Enum, schema.Required
}

var (
	eventIDInResponse    = regexp.MustCompile(`"eventId"\s*:\s*"([^"]+)"`)
	calendarIDInResponse = regexp.MustCompile(`"calendarId"\s*:\s*"([^"]+)"`)
)

// assertOpaqueEventIDNeedsNoAccountID is the MCP-05 / SC#4 proof: the schema half
// is unconditional (the flat-union root only ever requires "action", so accountId
// can never be schema-required on ANY action including get_calendar_event_details).
// The round-trip half resolves an opaque eventId through to a detail call with
// no accountId argument. The live tier seeds a local JSON provider, so absence
// of an event is a failed fixture, never a green skip.
func assertOpaqueEventIDNeedsNoAccountID(t *testing.T, ctx context.Context, session *sdkmcp.ClientSession, tool *sdkmcp.Tool) {
	t.Helper()

	_, required := calendarActionSchema(t, tool)
	if slices.Contains(required, "accountId") {
		t.Fatalf("curated tool schema marks accountId required at the root — MCP-05 regressed: %v", required)
	}

	// Pin the query to the fixture's own 2026-08-24..25 window. The provider's
	// omitted-range default starts at the wall clock's current day, which made this
	// permanent fixture silently expire on 2026-08-26.
	out, isErr := callCalendar(t, ctx, session, map[string]any{
		"action":    "get_calendar_events",
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

	detailArgs := map[string]any{
		"action":     "get_calendar_event_details",
		"eventId":    eventMatch[1],
		"calendarId": calendarMatch[1],
		"timeZone":   "UTC",
	}
	if _, hasAccountID := detailArgs["accountId"]; hasAccountID {
		t.Fatal("test bug: the detail call's dispatched arguments must not include accountId")
	}
	detailOut, detailIsErr := callCalendar(t, ctx, session, detailArgs)
	if detailIsErr {
		t.Fatalf("get_calendar_event_details with the opaque eventId reference (no accountId argument) failed: %s", detailOut)
	}
	t.Logf("MCP-05 round-trip proven live: eventId reference resolved to event detail with no accountId argument dispatched")
}
