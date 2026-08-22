package mcptools

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"sort"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/mcp/manager"
)

func TestMultiplexActionClassForCalendarActions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		tool   string
		action string
		want   MCPActionClass
	}{
		{"destructive send", CalendarMultiplexedToolName, "send_email", MCPActionDestructive},
		{"read", CalendarMultiplexedToolName, "list_calendars", MCPActionRead},
		{"mutate", CalendarMultiplexedToolName, "create_event", MCPActionMutate},
		{"unrecognised action", CalendarMultiplexedToolName, "not_an_action", MCPActionUnknown},
		{"unrelated tool never borrows the calendar table", "some__unknown_tool", "list_calendars", MCPActionUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := MCPActionClassFor(tt.tool, tt.action); got != tt.want {
				t.Fatalf("MCPActionClassFor(%q, %q) = %v, want %v", tt.tool, tt.action, got, tt.want)
			}
		})
	}
}

func TestMultiplexMCPToolsListedSortedAndNamespaced(t *testing.T) {
	t.Parallel()
	got := MultiplexedMCPTools()
	if len(got) == 0 {
		t.Fatal("MultiplexedMCPTools() returned nothing")
	}
	if !sort.StringsAreSorted(got) {
		t.Fatalf("MultiplexedMCPTools() not sorted: %v", got)
	}
	for _, name := range got {
		if name == "" {
			t.Fatal("MultiplexedMCPTools() contains an empty name")
		}
		if !strings.Contains(name, "__") {
			t.Errorf("%q does not contain the __ namespace delimiter", name)
		}
	}
}

func TestMultiplexIsKnownMultiplexedMCPTool(t *testing.T) {
	t.Parallel()
	if !isKnownMultiplexedMCPTool(CalendarMultiplexedToolName) {
		t.Fatalf("%q must be a known multiplexed tool", CalendarMultiplexedToolName)
	}
	if isKnownMultiplexedMCPTool("sb__sandbox_exec") {
		t.Fatal("sb__sandbox_exec must not be a known multiplexed tool")
	}
}

// TestMultiplexSpecFromToolDefSetsMultiplexedForCuratedCalendarTool is the
// load-bearing D-21 wiring proof: bridging the curated calendar tool must yield
// Multiplexed:true (so gateway.ValidateClassifiable actually inspects it) AND
// Mutating:true (the fail-closed default classifyToolRisk falls through to,
// because the curated tool's own raw name "calendar" is not a key in
// trustedRecipeActions[calendarRecipeSource] — that table is keyed by ACTION).
func TestMultiplexSpecFromToolDefSetsMultiplexedForCuratedCalendarTool(t *testing.T) {
	t.Parallel()
	policy := managedBridgePolicy(mcp.ManagedServer{
		Source: calendarRecipeSource,
		Trust:  mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
	})
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{"type": "string", "enum": []any{"list_accounts", "send_email"}},
		},
		"required": []any{"action"},
	}
	spec := specFromToolDefWithPolicy(calendarNamespace, mustTool(calendarCuratedTool, "Curated calendar tool.", schema, nil), policy)

	if spec.Name != CalendarMultiplexedToolName {
		t.Fatalf("spec.Name = %q, want %q", spec.Name, CalendarMultiplexedToolName)
	}
	if !spec.Multiplexed {
		t.Fatal("curated calendar tool must set Multiplexed:true")
	}
	if !spec.Mutating {
		t.Fatal("curated calendar tool must fail closed to Mutating:true (its raw tool name \"calendar\" is not a trustedRecipeActions key, so classifyToolRisk falls through to the fail-closed default)")
	}
	if spec.OperationScope == "" || spec.OperationNormalizer == "" || spec.ReplayPolicy == "" {
		t.Fatalf("curated calendar tool has incomplete operation metadata: %+v", spec)
	}
}

// TestMultiplexSpecNotInferredFromSchemaShape pins D-34: a tool with NO
// classifier entry must never be inferred Multiplexed just because its schema
// happens to carry an `action` property — that inference would panic Aura's boot
// for any stranger's server whose schema looks like Aura's own multiplexed tools.
func TestMultiplexSpecNotInferredFromSchemaShape(t *testing.T) {
	t.Parallel()
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{"type": "string"},
		},
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("specFromToolDefWithPolicy panicked on a stranger's action-shaped schema: %v", r)
		}
	}()
	spec := specFromToolDefWithPolicy("stranger", mustTool("do_thing", "A stranger's tool.", schema, nil), bridgePolicy{})
	if spec.Multiplexed {
		t.Fatal("a stranger's server must never be inferred Multiplexed from schema shape alone")
	}
}

// TestMultiplexCalendarNamespaceMatchesCatalog guards against a catalog rename
// silently orphaning CalendarMultiplexedToolName's namespace half.
func TestMultiplexCalendarNamespaceMatchesCatalog(t *testing.T) {
	t.Parallel()
	entry, ok := manager.LookupCatalog("calendar")
	if !ok {
		t.Fatal("no built-in catalog entry named \"calendar\"")
	}
	if entry.Name != calendarNamespace {
		t.Fatalf("catalog entry Name = %q, want calendarNamespace %q", entry.Name, calendarNamespace)
	}
}

// TestCuratedActionReconciliationWarnsBothDirections is D-33's proof: an action
// the schema advertises but the table does not know, and a table entry the
// schema no longer advertises, are BOTH reported by name — mirroring
// bridge_risk_test.go's TestMemoryRecipeCoversEveryServedTool's two-way shape.
func TestCuratedActionReconciliationWarnsBothDirections(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(old) })

	// Drop "send_email" (a real table entry) and add "new_forked_action" (unknown
	// to the table) — both directions must be reported by name.
	enum := make([]string, 0, len(trustedRecipeActions[calendarRecipeSource]))
	for action := range trustedRecipeActions[calendarRecipeSource] {
		if action == "send_email" {
			continue
		}
		enum = append(enum, action)
	}
	enum = append(enum, "new_forked_action")
	params, err := json.Marshal(map[string]any{
		"properties": map[string]any{"action": map[string]any{"enum": enum}},
	})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	reconcileCuratedActions("calendar__calendar", calendarRecipeSource, params)

	out := logs.String()
	if !strings.Contains(out, "new_forked_action") {
		t.Errorf("missing WARN naming the schema action absent from the table: %s", out)
	}
	if !strings.Contains(out, "send_email") {
		t.Errorf("missing WARN naming the table entry the schema no longer advertises: %s", out)
	}
}

// TestCuratedActionEnumToleratesAbsentOrUnparseable proves D-33's "never panic"
// half: no action enum, or unparseable params, must emit nothing and must not panic.
func TestCuratedActionEnumToleratesAbsentOrUnparseable(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(old) })

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("reconcileCuratedActions panicked: %v", r)
		}
	}()
	reconcileCuratedActions("x__y", calendarRecipeSource, json.RawMessage(`{"type":"object"}`))
	reconcileCuratedActions("x__y", calendarRecipeSource, json.RawMessage(`not json`))
	reconcileCuratedActions("x__y", calendarRecipeSource, nil)

	if logs.Len() != 0 {
		t.Errorf("expected no WARN for an absent/unparseable action enum, got: %s", logs.String())
	}
}
