package gateway

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/scoring"
)

// curatedCalendarSpec is the shape classify.go's classifyCalendarAction
// dispatches against. Mutating/Multiplexed/operation-metadata are set the same
// way bridge.go's specFromToolDefWithPolicy would set them for the real curated
// tool, so a fixture drift here is visible against TestValidateClassifiableAcceptsCuratedCalendarTool.
func curatedCalendarSpec() tools.Spec {
	return tools.Spec{
		Name:                mcptools.CalendarMultiplexedToolName,
		Mutating:            true,
		Multiplexed:         true,
		OperationScope:      tools.OperationScopeMCP,
		OperationNormalizer: tools.OperationNormalizerCanonical,
		ReplayPolicy:        tools.ReplayToolResult,
	}
}

// calendarDesignDocActions mirrors internal/agent/mcptools/bridge_risk_test.go's
// own hard-coded copy (docs/superpowers/specs/2026-08-17-mcp-curated-surface-design.md
// §5a) as an independent literal, not an import — so a change to either copy
// without the other fails a test instead of both drifting together silently.
var calendarDesignDocActions = []string{
	"list_accounts", "get_emails", "get_email_details", "search_emails",
	"list_calendars", "get_calendar_events", "get_calendar_event_details",
	"get_contacts", "search_contacts", "get_contact_details",
	"create_event", "update_event", "respond_to_event", "send_email",
}

func TestClassifyCalendarActionTiers(t *testing.T) {
	spec := curatedCalendarSpec()
	tests := []struct {
		name string
		args json.RawMessage
		want scoring.RiskTier
	}{
		{"read", json.RawMessage(`{"action":"list_calendars"}`), scoring.Safe},
		{"mutate", json.RawMessage(`{"action":"create_event"}`), scoring.Normal},
		{"destructive send", json.RawMessage(`{"action":"send_email"}`), scoring.Destructive},
		{"destructive respond", json.RawMessage(`{"action":"respond_to_event"}`), scoring.Destructive},
		{"mail mutate mark read", json.RawMessage(`{"action":"mark_email_read"}`), scoring.Normal},
		{"mail destructive delete", json.RawMessage(`{"action":"delete_email"}`), scoring.Destructive},
		// move_email must NOT be cheaper than delete_email: 'trash' is an accepted
		// destination, so a Mutate tier here would be a bypass of delete_email's gate.
		{"mail destructive move", json.RawMessage(`{"action":"move_email"}`), scoring.Destructive},
		{"mail move to trash is not cheaper than delete", json.RawMessage(`{"action":"move_email","destination":"trash"}`), scoring.Destructive},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classify(spec, tc.args); got != tc.want {
				t.Fatalf("classify(%s) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

func TestClassifyCalendarActionFailsSafe(t *testing.T) {
	spec := curatedCalendarSpec()
	tests := []struct {
		name string
		args json.RawMessage
	}{
		{"unknown action", json.RawMessage(`{"action":"totally_unknown"}`)},
		{"empty action", json.RawMessage(`{"action":""}`)},
		{"missing action key", json.RawMessage(`{}`)},
		{"unparseable json", json.RawMessage(`not json`)},
		{"nil args", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classify(spec, tc.args); got != scoring.Risky {
				t.Fatalf("classify(%s) = %q, want %q", tc.name, got, scoring.Risky)
			}
		})
	}
}

// TestClassifyCalendarActionExhaustive is the table-driven exhaustiveness case:
// every action the design doc lists for calendar must classify to a graduated
// (non-Risky) tier, so an action added to the table without wiring a class goes
// red instead of silently riding the unknown-action Risky default.
func TestClassifyCalendarActionExhaustive(t *testing.T) {
	spec := curatedCalendarSpec()
	for _, action := range calendarDesignDocActions {
		t.Run(action, func(t *testing.T) {
			args, err := json.Marshal(map[string]string{"action": action})
			if err != nil {
				t.Fatalf("marshal args: %v", err)
			}
			if got := classify(spec, args); got == scoring.Risky {
				t.Fatalf("classify(%s) = Risky, want a graduated tier", action)
			}
		})
	}
}

func TestClassifyCalendarActionIdempotent(t *testing.T) {
	spec := curatedCalendarSpec()
	args := json.RawMessage(`{"action":"send_email"}`)
	first := classify(spec, args)
	second := classify(spec, args)
	if first != second {
		t.Fatalf("classify not idempotent: %q then %q", first, second)
	}
}

// TestClassifyCalendarActionConcurrent proves the classifier reads only
// read-only-after-init state (trustedRecipeActions, multiplexedMCPTools,
// multiplexedClassifiers) — run under -race.
func TestClassifyCalendarActionConcurrent(t *testing.T) {
	spec := curatedCalendarSpec()
	args := json.RawMessage(`{"action":"send_email"}`)
	const n = 100
	results := make([]scoring.RiskTier, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			results[i] = classify(spec, args)
		}(i)
	}
	wg.Wait()
	for i, r := range results {
		if r != scoring.Destructive {
			t.Fatalf("goroutine %d classified %q, want %q", i, r, scoring.Destructive)
		}
	}
}

// TestEveryMultiplexedMCPToolHasAClassifier is the bidirectional invariant: every
// name mcptools.MultiplexedMCPTools() knows about must have exactly one
// multiplexedClassifiers entry, and no MCP-namespaced (contains "__") classifier
// entry may exist that mcptools does not also know about — failing if EITHER
// side is extended alone. skill_manage/task/swarm_spawn are excluded from the
// reverse direction because they carry no "__" namespace delimiter.
func TestEveryMultiplexedMCPToolHasAClassifier(t *testing.T) {
	t.Parallel()
	want := make(map[string]bool)
	for _, name := range mcptools.MultiplexedMCPTools() {
		want[name] = true
	}
	got := make(map[string]bool)
	for name := range multiplexedClassifiers {
		if strings.Contains(name, "__") {
			got[name] = true
		}
	}
	for name := range want {
		if !got[name] {
			t.Errorf("mcptools knows %q but multiplexedClassifiers has no entry for it", name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("multiplexedClassifiers has %q but mcptools.MultiplexedMCPTools() does not know it", name)
		}
	}
}

// TestValidateClassifiableAcceptsCuratedCalendarTool proves the boot guard is
// satisfied end to end once the classifier is registered: a registry holding the
// real curated calendar spec must not panic.
func TestValidateClassifiableAcceptsCuratedCalendarTool(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(fakeTool{spec: curatedCalendarSpec()})

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("ValidateClassifiable panicked on the curated calendar tool: %v", r)
		}
	}()
	ValidateClassifiable(reg)
}
