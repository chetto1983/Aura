package pimsplit

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/messagedrafts"
)

func TestNamespacesAreTheRecipeRowsAndTheDefaultMount(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		servers []server
		want    []string
	}{
		{"no row: the appliance mounts the catalog recipe", nil, []string{"calendar"}},
		{"an alias beside the default", []server{{"work-pim", "recipe:calendar"}, {"notes", "custom"}},
			[]string{"calendar", "work-pim"}},
		{"the recipe materialised under its own name", []server{{"calendar", "recipe:calendar"}}, []string{"calendar"}},
		{"a custom server owns the name calendar", []server{{"calendar", "custom"}, {"pim", "recipe:calendar"}},
			[]string{"pim"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := namespacesOf(tc.servers); !slices.Equal(got, tc.want) {
				t.Fatalf("namespacesOf = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDecideMovesEachStoredRow(t *testing.T) {
	t.Parallel()
	cases := []struct {
		action, kind string
		want         move
		reason       string
	}{
		{"send_email", "grant", moveRewrite, ""},
		{"delete_event", "ask", moveRewrite, ""},
		{"send_email", "deny", moveRewrite, ""},
		{"", "deny", moveFanOut, ""},
		{"", "grant", moveDrop, "tool-wide"},
		{"", "ask", moveDrop, "tool-wide"},
		{"frobnicate", "grant", moveDrop, "unknown action"},
		{"calendar", "deny", moveDrop, "unknown action"},
	}
	for _, tc := range cases {
		got, reason := decide(tc.action, tc.kind)
		if got != tc.want || !strings.Contains(reason, tc.reason) {
			t.Errorf("decide(%q, %q) = %v %q, want %v containing %q", tc.action, tc.kind, got, reason, tc.want, tc.reason)
		}
	}
}

const (
	plainSendArgs   = `{"accountId":"acc-1","to":["a@example.com"],"subject":"Hi","body":"Text","bodyFormat":"text"}`
	plainLegacyArgs = `{"to": ["a@example.com"], "body": "Text", "action": "send_email", "subject": "Hi", "accountId": "acc-1", "bodyFormat": "text"}`
)

func expectedFingerprint(t *testing.T, args string) string {
	t.Helper()
	merged, err := messagedrafts.MergeApprovedArgs(messagedrafts.EmailTarget(), json.RawMessage(args), nil)
	if err != nil {
		t.Fatal(err)
	}
	fp, err := messagedrafts.Fingerprint(messagedrafts.EmailTarget(), merged)
	if err != nil {
		t.Fatal(err)
	}
	return fp
}

func TestPlanDraftRewritesAnAccountedDraft(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("p", 60)
	plan := planDraft([]string{"calendar", long}, draftRow{
		registered: mcptools.NamespacedName(long, "calendar"),
		original:   []byte(plainLegacyArgs),
		effective:  []byte(plainLegacyArgs),
	})
	if plan.expire != "" {
		t.Fatalf("expired: %s", plan.expire)
	}
	if plan.registered != mcptools.NamespacedName(long, "send_email") {
		t.Fatalf("registered = %q", plan.registered)
	}
	want := expectedFingerprint(t, plainSendArgs)
	if plan.originalFP != want || plan.effectiveFP != want {
		t.Fatalf("fingerprints = %s %s, want %s", plan.originalFP, plan.effectiveFP, want)
	}
	if strings.Contains(string(plan.original), "action") || strings.Contains(string(plan.effective), "action") {
		t.Fatalf("action survived: %s %s", plan.original, plan.effective)
	}
}

func TestPlanDraftLeavesAMissingEffectiveMissing(t *testing.T) {
	t.Parallel()
	plan := planDraft([]string{"calendar"}, draftRow{registered: "calendar__calendar", original: []byte(plainLegacyArgs)})
	if plan.expire != "" || plan.effective != nil || plan.effectiveFP != "" {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestPlanDraftExpiresWhatCannotBeSent(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		row  draftRow
		want string
	}{
		{"no account", draftRow{registered: "calendar__calendar",
			original: []byte(`{"to":["a@example.com"],"action":"send_email","subject":"Hi","body":"x"}`)}, accountRequired},
		{"a blank account", draftRow{registered: "calendar__calendar",
			original: []byte(`{"to":["a@example.com"],"action":"send_email","subject":"Hi","accountId":"  "}`)}, accountRequired},
		{"an account only in the original: the effective args are what is sent", draftRow{registered: "calendar__calendar",
			original:  []byte(plainLegacyArgs),
			effective: []byte(`{"to":["a@example.com"],"action":"send_email","subject":"Hi"}`)}, accountRequired},
		{"its server is no PIM mount", draftRow{registered: "gone__calendar", original: []byte(plainLegacyArgs)}, unmappable},
		{"original args the send refuses", draftRow{registered: "calendar__calendar",
			original: []byte(`{"to":["a@example.com"],"subject":"Hi","accountId":"acc-1","replyTo":"x"}`)}, unmappable},
		{"effective args the send refuses", draftRow{registered: "calendar__calendar",
			original: []byte(plainLegacyArgs), effective: []byte(`{"to":"a@example.com","subject":"Hi","accountId":"acc-1"}`)}, unmappable},
		{"args that are not an object", draftRow{registered: "calendar__calendar", original: []byte(`[1]`)}, unmappable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := planDraft([]string{"calendar"}, tc.row).expire; got != tc.want {
				t.Fatalf("expire = %q, want %q", got, tc.want)
			}
		})
	}
}
