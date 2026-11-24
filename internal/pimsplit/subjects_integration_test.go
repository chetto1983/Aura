//go:build db_integration

package pimsplit

import (
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/mcptools"
)

func TestPerActionGrantBecomesAToolGrant(t *testing.T) {
	h := newHarness(t)
	id := h.identity()
	h.grant(id, oldTool, "send_email")
	h.policy(id, oldTool, "delete_event", "ask")
	if report := h.upgrade(); report != (Report{Rewritten: 2}) {
		t.Fatalf("Report = %+v", report)
	}
	if got := h.grants(id); !slices.Equal(got, []string{"calendar__send_email/ by alice"}) {
		t.Fatalf("grants = %v", got)
	}
	if got := h.policies(id); !slices.Equal(got, []string{"calendar__delete_event/=ask by bob"}) {
		t.Fatalf("policies = %v", got)
	}
}

func everyToolDenied(namespace, by string) []string {
	var want []string
	for _, name := range mcptools.CalendarToolNames() {
		want = append(want, mcptools.NamespacedName(namespace, name)+"/=deny by "+by)
	}
	slices.Sort(want)
	return want
}

func TestToolWideDenyFansOutToEveryTool(t *testing.T) {
	h := newHarness(t)
	id := h.identity()
	h.policy(id, oldTool, "", "deny")
	if report := h.upgrade(); report != (Report{FannedOutDenies: 1}) {
		t.Fatalf("Report = %+v", report)
	}
	got := h.policies(id)
	if len(got) != 29 || !slices.Equal(got, everyToolDenied("calendar", "bob")) {
		t.Fatalf("policies = %v", got)
	}
}

func TestToolWideGrantAndAskAreLoggedAndDropped(t *testing.T) {
	h := newHarness(t)
	id := h.identity()
	h.grant(id, oldTool, "")
	h.policy(id, oldTool, "", "ask")
	if report := h.upgrade(); report != (Report{Dropped: 2}) {
		t.Fatalf("Report = %+v", report)
	}
	if g, p := h.grants(id), h.policies(id); len(g)+len(p) != 0 {
		t.Fatalf("left behind: %v %v", g, p)
	}
	logs := h.logs.String()
	for _, want := range []string{"tool=calendar__calendar action=\"\" kind=grant", "tool=calendar__calendar action=\"\" kind=ask"} {
		if !strings.Contains(logs, want) {
			t.Errorf("the log does not name %q:\n%s", want, logs)
		}
	}
}

// A row already named after a PIM tool before the split matched no tool, so nobody saw it act. It
// is not switched on by the tool arriving.
func TestHandTypedNewNameRowIsDropped(t *testing.T) {
	h := newHarness(t)
	id := h.identity()
	h.grant(id, "calendar__send_email", "")
	h.policy(id, "calendar__send_email", "", "ask")
	h.grant(id, "notes__search", "")
	if report := h.upgrade(); report != (Report{Dropped: 2}) {
		t.Fatalf("Report = %+v", report)
	}
	if got := h.grants(id); !slices.Equal(got, []string{"notes__search/ by alice"}) {
		t.Fatalf("grants = %v", got)
	}
	if got := h.policies(id); len(got) != 0 {
		t.Fatalf("policies = %v", got)
	}
	if !strings.Contains(h.logs.String(), "tool=calendar__send_email") {
		t.Fatalf("the hand-typed row is not in the log:\n%s", h.logs.String())
	}
}

// The gateway stored the model's raw action string; one the PIM never had maps to no tool.
func TestUnknownActionIsDropped(t *testing.T) {
	h := newHarness(t)
	id := h.identity()
	h.grant(id, oldTool, "frobnicate")
	h.policy(id, oldTool, "send_email; drop", "ask")
	if report := h.upgrade(); report != (Report{Dropped: 2}) {
		t.Fatalf("Report = %+v", report)
	}
	if g, p := h.grants(id), h.policies(id); len(g)+len(p) != 0 {
		t.Fatalf("left behind: %v %v", g, p)
	}
	if !strings.Contains(h.logs.String(), "action=frobnicate") {
		t.Fatalf("the unknown action is not in the log:\n%s", h.logs.String())
	}
}

func TestDenyWinsOverARewrittenAsk(t *testing.T) {
	h := newHarness(t)
	id := h.identity()
	h.policy(id, oldTool, "send_email", "ask")
	h.policy(id, oldTool, "", "deny")
	if report := h.upgrade(); report != (Report{Rewritten: 1, FannedOutDenies: 1}) {
		t.Fatalf("Report = %+v", report)
	}
	if got := h.policies(id); !slices.Equal(got, everyToolDenied("calendar", "bob")) {
		t.Fatalf("policies = %v", got)
	}
}

// An aliased mount is found by the name the bridge gives its calendar tool, hashed when long, and
// a server whose tool merely ends in __calendar is not the PIM.
func TestAliasIsMatchedByNamespacedName(t *testing.T) {
	h := newHarness(t)
	long := strings.Repeat("p", 60)
	h.server("work-pim", "recipe:calendar")
	h.server(long, "recipe:calendar")
	h.server("notes", "custom")
	hashed := mcptools.NamespacedName(long, "calendar")
	if hashed == long+"__calendar" {
		t.Fatalf("the long alias is not hashed: %s", hashed)
	}
	id := h.identity()
	h.grant(id, mcptools.NamespacedName("work-pim", "calendar"), "send_email")
	h.policy(id, hashed, "delete_email", "ask")
	h.grant(id, "notes__calendar", "send_email")
	if report := h.upgrade(); report != (Report{Rewritten: 2}) {
		t.Fatalf("Report = %+v", report)
	}
	wantGrants := []string{"notes__calendar/send_email by alice", "work-pim__send_email/ by alice"}
	if got := h.grants(id); !slices.Equal(got, wantGrants) {
		t.Fatalf("grants = %v, want %v", got, wantGrants)
	}
	if got := h.policies(id); !slices.Equal(got, []string{mcptools.NamespacedName(long, "delete_email") + "/=ask by bob"}) {
		t.Fatalf("policies = %v", got)
	}
}

// The appliance mounts the calendar recipe by default with no registry row (manager.RuntimeSet).
func TestDefaultMountWithoutARowIsCovered(t *testing.T) {
	h := newHarness(t)
	if n := h.strings(`SELECT name FROM aura.mcp_server WHERE name = 'calendar' OR source = 'recipe:calendar'`); len(n) != 0 {
		t.Fatalf("the database already registers a PIM: %v", n)
	}
	id := h.identity()
	h.grant(id, oldTool, "send_email")
	if report := h.upgrade(); report != (Report{Rewritten: 1}) {
		t.Fatalf("Report = %+v", report)
	}
	if got := h.grants(id); !slices.Equal(got, []string{"calendar__send_email/ by alice"}) {
		t.Fatalf("grants = %v", got)
	}
}

// An operator's own server named calendar takes the name from the recipe, which is then not mounted.
func TestACustomServerNamedCalendarIsNotThePIM(t *testing.T) {
	h := newHarness(t)
	h.server("calendar", "custom")
	id := h.identity()
	h.grant(id, oldTool, "send_email")
	h.grant(id, "calendar__send_email", "")
	if report := h.upgrade(); report != (Report{}) {
		t.Fatalf("Report = %+v", report)
	}
	want := []string{"calendar__calendar/send_email by alice", "calendar__send_email/ by alice"}
	if got := h.grants(id); !slices.Equal(got, want) {
		t.Fatalf("grants = %v, want %v", got, want)
	}
}
