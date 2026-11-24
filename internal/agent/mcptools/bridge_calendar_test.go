package mcptools

import (
	"log/slog"
	"slices"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
)

func calendarPolicy() bridgePolicy {
	return managedBridgePolicy(mcp.ManagedServer{
		Source: calendarRecipeSource,
		Trust:  mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
	})
}

// TestUnknownCalendarToolIsDestructiveDespiteReadOnlyHint keeps the floor an unknown
// action had before the split: a PIM tool the risk table does not name is a tool nobody
// tiered, so the fork's own readOnlyHint must not lower it.
func TestUnknownCalendarToolIsDestructiveDespiteReadOnlyHint(t *testing.T) {
	t.Parallel()
	tool := mustTool("brand_new", "A tool the fork added.", nil, &sdkmcp.ToolAnnotations{ReadOnlyHint: true})
	spec := specFromToolDefWithPolicy("calendar", tool, calendarPolicy())
	if !spec.Mutating || !spec.Destructive {
		t.Fatalf("unknown calendar tool = mutating:%v destructive:%v, want true/true", spec.Mutating, spec.Destructive)
	}
}

// TestCalendarReadToolIsNotMutating is the point of the split: a read leaves the
// operation-reservation funnel, which only a mutating spec enters.
func TestCalendarReadToolIsNotMutating(t *testing.T) {
	t.Parallel()
	spec := specFromToolDefWithPolicy("calendar", mustTool("get_emails", "", nil, nil), calendarPolicy())
	if spec.Mutating || spec.Destructive {
		t.Fatalf("get_emails = mutating:%v destructive:%v, want false/false", spec.Mutating, spec.Destructive)
	}
	if spec.OperationScope != "" {
		t.Fatalf("get_emails operation scope = %q, want none", spec.OperationScope)
	}
}

func TestCalendarSendIsDestructive(t *testing.T) {
	t.Parallel()
	tool := mustTool("send_email", "", nil, &sdkmcp.ToolAnnotations{ReadOnlyHint: true})
	spec := specFromToolDefWithPolicy("calendar", tool, calendarPolicy())
	if !spec.Mutating || !spec.Destructive {
		t.Fatalf("send_email = mutating:%v destructive:%v, want true/true", spec.Mutating, spec.Destructive)
	}
	if spec.OperationScope != tools.OperationScopeMCP {
		t.Fatalf("send_email operation scope = %q, want %q", spec.OperationScope, tools.OperationScopeMCP)
	}
}

// TestCalendarToolNameDriftWarns: the PIM's tool names live in another repository and can
// drift without Aura noticing. A name the fork advertises that the risk table lacks, and a
// table key the fork no longer advertises, are each reported by name at mount.
func TestCalendarToolNameDriftWarns(t *testing.T) {
	logs := captureLogs(t, slog.LevelWarn)
	var advertised []*sdkmcp.Tool
	for _, name := range CalendarToolNames() {
		if name != "send_email" {
			advertised = append(advertised, mustTool(name, "", nil, nil))
		}
	}
	advertised = append(advertised, mustTool("brand_new", "", nil, nil))

	bridgeToolsWithPolicy("calendar", nil, advertised, defaultMCPCallTimeout, calendarPolicy())

	out := logs.String()
	if !strings.Contains(out, "tool=brand_new") {
		t.Errorf("missing WARN naming the advertised tool absent from the risk table: %s", out)
	}
	if !strings.Contains(out, "tool=send_email") {
		t.Errorf("missing WARN naming the table key the fork no longer advertises: %s", out)
	}
}

// TestCalendarToolNameDriftIsSilentInStep asserts the absence the test above cannot: a fork
// in step with the table warns about nothing. A drift warning that fires on a healthy mount
// is one an operator learns to ignore.
func TestCalendarToolNameDriftIsSilentInStep(t *testing.T) {
	logs := captureLogs(t, slog.LevelWarn)
	var advertised []*sdkmcp.Tool
	for _, name := range CalendarToolNames() {
		advertised = append(advertised, mustTool(name, "", nil, nil))
	}

	bridgeToolsWithPolicy("calendar", nil, advertised, defaultMCPCallTimeout, calendarPolicy())

	if logs.Len() != 0 {
		t.Errorf("a fork in step with the risk table must warn about nothing, got: %s", logs.String())
	}
}

// calendarForkTools is the PIM fork's tool list, hard-coded rather than derived from
// trustedRecipeActions, so a table edit that drops or adds a tool fails here instead of
// passing vacuously. Upstream serves these 29 tools; the fork advertises each under its
// own name (docs/superpowers/specs/2026-11-23-pim-tool-split-design.md §4.1).
var calendarForkTools = []string{
	"list_accounts", "get_emails", "get_email_details", "search_emails",
	"list_calendars", "get_calendar_events", "get_calendar_event_details",
	"get_contacts", "search_contacts", "get_contact_details",
	"create_event", "update_event", "respond_to_event", "send_email",
	"mark_email_read", "delete_email", "move_email",
	// delete_event was once absent while create_event and update_event were present, so
	// an agent could add an event to a calendar and never remove it.
	"delete_event", "create_contact", "update_contact", "delete_contact",
	"get_email_attachment", "get_contextual_email_summary", "get_guide",
	"get_unsubscribe_info", "unsubscribe_from_email",
	"bulk_delete_emails", "bulk_mark_emails_read", "bulk_move_emails",
}

func TestCalendarToolNamesAreTheForksTools(t *testing.T) {
	t.Parallel()
	want := slices.Sorted(slices.Values(calendarForkTools))
	if got := CalendarToolNames(); !slices.Equal(got, want) {
		t.Fatalf("CalendarToolNames() = %v, want %v", got, want)
	}
}
