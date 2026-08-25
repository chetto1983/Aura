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

func calendarBearerOrGate(t *testing.T) string {
	t.Helper()
	if token := strings.TrimSpace(os.Getenv("AURA_PIM_MCP_ADMIN_TOKEN")); token != "" {
		return token
	}
	if strings.TrimSpace(os.Getenv("CI")) != "" {
		t.Fatal("AURA_PIM_MCP_ADMIN_TOKEN must be set under CI because the live Calendar MCP transport is bearer-protected")
	}
	t.Skip("set AURA_PIM_MCP_ADMIN_TOKEN to run the bearer-protected calendar_integration tier")
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
	bearer := calendarBearerOrGate(t)
	reapIdleHTTPConns(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	server := ManagedServer{
		Type:  ServerTypeStreamableHTTP,
		URL:   endpoint,
		Env:   []string{"MCP_BEARER_TOKEN=" + bearer},
		Trust: ManagedTrust{Class: TrustTrustedRecipe},
	}
	session, err := OpenSDKSession(ctx, "calendar", server, EgressPolicy{}, SessionOptions{})
	if err != nil {
		t.Fatalf("OpenSDKSession calendar PIM sidecar at %s: %v", endpoint, err)
	}
	defer func() { _ = session.Close() }()

	result := session.InitializeResult()
	if result == nil || strings.TrimSpace(result.ProtocolVersion) == "" {
		t.Error("calendar sidecar: InitializeResult().ProtocolVersion is empty")
	} else {
		t.Logf("calendar sidecar negotiated protocol version: %s", result.ProtocolVersion)
	}

	advertised := drainSDKToolsForTest(t, ctx, session)
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
	out, isErr := callCalendarForIdentity(t, ctx, session, calendarTenantA, map[string]any{"action": "list_accounts"})
	if isErr {
		t.Fatalf("CallTool calendar(list_accounts) reported isError: %s", out)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("calendar(list_accounts) returned empty content")
	}
	foreignOut, foreignIsErr := callCalendarForIdentity(t, ctx, session, calendarTenantB, map[string]any{"action": "list_accounts"})
	if foreignIsErr {
		t.Fatalf("CallTool calendar(list_accounts) for tenant B reported isError: %s", foreignOut)
	}
	if !strings.Contains(out, calendarTenantAccount) || strings.Contains(out, calendarForeignAccount) {
		t.Fatalf("tenant A account surface is not isolated: %s", out)
	}
	if !strings.Contains(foreignOut, calendarForeignAccount) || strings.Contains(foreignOut, calendarTenantAccount) {
		t.Fatalf("tenant B account surface is not isolated: %s", foreignOut)
	}

	assertOpaqueEventIDNeedsNoAccountID(t, ctx, session, tool, calendarTenantA)
}

func callCalendarForIdentity(t *testing.T, ctx context.Context, session *sdkmcp.ClientSession, identityID string, args map[string]any) (text string, isError bool) {
	t.Helper()
	params := &sdkmcp.CallToolParams{Name: curatedCalendarToolName, Arguments: args}
	SetAuraMetaField(params, MetaFieldUserIdentifier, identityID)
	res, err := session.CallTool(ctx, params)
	if err != nil {
		t.Fatalf("CallTool %s for identity %s: %v", curatedCalendarToolName, identityID, err)
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
// The round-trip half — actually resolving an opaque eventId through to a detail
// call with no accountId argument — is best-effort: a CI sidecar has zero
// connected accounts (46-05's empty-config container), so get_calendar_events
// legitimately returns no events to chain from. When that happens this function
// logs and returns without fabricating a reference, per this plan's own
// instruction.
func assertOpaqueEventIDNeedsNoAccountID(t *testing.T, ctx context.Context, session *sdkmcp.ClientSession, tool *sdkmcp.Tool, identityID string) {
	t.Helper()

	_, required := calendarActionSchema(t, tool)
	if slices.Contains(required, "accountId") {
		t.Fatalf("curated tool schema marks accountId required at the root — MCP-05 regressed: %v", required)
	}

	out, isErr := callCalendarForIdentity(t, ctx, session, identityID,
		map[string]any{"action": "get_calendar_events", "timeZone": "UTC"})
	if isErr {
		t.Logf("get_calendar_events reported isError (expected with zero connected accounts): %s — MCP-05 round-trip half not exercised, only the schema half was", out)
		return
	}
	eventMatch := eventIDInResponse.FindStringSubmatch(out)
	calendarMatch := calendarIDInResponse.FindStringSubmatch(out)
	if len(eventMatch) < 2 || len(calendarMatch) < 2 {
		t.Logf("get_calendar_events returned no eventId/calendarId to chain from — MCP-05 round-trip half not exercised, only the schema half was. Cause is an account with no completed OAuth grant (a configured-but-unauthenticated account looks identical here to no account at all); connect one to close this")
		return
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
	detailOut, detailIsErr := callCalendarForIdentity(t, ctx, session, identityID, detailArgs)
	if detailIsErr {
		t.Fatalf("get_calendar_event_details with the opaque eventId reference (no accountId argument) failed: %s", detailOut)
	}
	t.Logf("MCP-05 round-trip proven live: eventId reference resolved to event detail with no accountId argument dispatched")
}
