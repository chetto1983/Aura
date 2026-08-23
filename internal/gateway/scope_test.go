package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/agent/tools"
)

// TestSubjectForReadsTheVerbOfAMultiplexedToolOnly proves the grant key is the tool for an
// ordinary tool and tool+verb for an action-multiplexed one — the property amendment #127
// rests on, because a subject that dropped the verb would make "always approve calendar
// delete_event" silently authorize "calendar send_email" too.
func TestSubjectForReadsTheVerbOfAMultiplexedToolOnly(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		spec tools.Spec
		args string
		want grantSubject
	}{
		{"multiplexed tool carries its verb",
			tools.Spec{Name: "skill_manage"}, `{"action":"delete","name":"x"}`,
			grantSubject{Tool: "skill_manage", Action: "delete"}},
		{"curated calendar tool carries its verb",
			tools.Spec{Name: mcptools.CalendarMultiplexedToolName}, `{"action":"delete_event","eventId":"e-1"}`,
			grantSubject{Tool: mcptools.CalendarMultiplexedToolName, Action: "delete_event"}},
		// An ordinary tool has no verb, so its arguments are never read for one: an
		// `action` field on shell_exec is the tool's own argument, not a verb.
		{"plain tool ignores an action-looking argument",
			tools.Spec{Name: "shell_exec"}, `{"action":"delete","command":"rm -rf /"}`,
			grantSubject{Tool: "shell_exec"}},
		// Fail-narrow: a payload the gateway cannot parse grants LESS than the call, never more.
		{"unparseable args degrade to the tool alone",
			tools.Spec{Name: "skill_manage"}, `{"action":`,
			grantSubject{Tool: "skill_manage"}},
		{"missing action degrades to the tool alone",
			tools.Spec{Name: "swarm_spawn"}, `{"goals":["a","b"]}`,
			grantSubject{Tool: "swarm_spawn"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := subjectFor(tc.spec, json.RawMessage(tc.args)); got != tc.want {
				t.Fatalf("subjectFor = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestScopeOptionLabelsAreRelayable proves the labels the model is told to copy verbatim are
// a payload ask_user accepts: 2-4 entries, non-empty, distinct. A set that failed that
// validation would surface as a tool error with the destructive call already withheld and no
// way left for the operator to answer.
func TestScopeOptionLabelsAreRelayable(t *testing.T) {
	t.Parallel()
	for _, subject := range []grantSubject{
		{Tool: "shell_exec"},
		{Tool: "skill_manage", Action: "delete"},
		{Tool: mcptools.CalendarMultiplexedToolName, Action: "delete_event"},
	} {
		labels := scopeOptionLabels(subject)
		if len(labels) < 2 || len(labels) > 4 {
			t.Fatalf("subject %q: %d labels, ask_user accepts 2-4", subject, len(labels))
		}
		seen := map[string]bool{}
		for _, l := range labels {
			if l == "" || seen[l] {
				t.Fatalf("subject %q: label %q is empty or repeated", subject, l)
			}
			seen[l] = true
		}
	}
}

// TestScopeForAnswerResolvesOnlyItsOwnLabels is the security property: a scope is granted
// ONLY by a label this subject generated. An empty answer, a reworded one, or a label
// generated for a DIFFERENT subject all resolve to ScopeOnce — so a model that rewrites the
// options, or replays a label from another approval, widens nothing.
func TestScopeForAnswerResolvesOnlyItsOwnLabels(t *testing.T) {
	t.Parallel()
	subject := grantSubject{Tool: "skill_manage", Action: "delete"}
	other := grantSubject{Tool: mcptools.CalendarMultiplexedToolName, Action: "delete_event"}

	for _, e := range scopeLabels(subject) {
		if got := scopeForAnswer(subject, e.Label); got != e.Scope {
			t.Errorf("scopeForAnswer(%q) = %q, want %q", e.Label, got, e.Scope)
		}
	}
	widening := []string{
		"", "approvo", "yes",
		"Approve once ", // trailing space — not the label
		"approve once",  // case-shifted — not the label
		"Always approve everything",
		"Approve skill_manage for this convo", // reworded
	}
	for _, e := range scopeLabels(other) {
		widening = append(widening, e.Label) // another subject's labels grant nothing here
	}
	for _, answer := range widening {
		if got := scopeForAnswer(subject, answer); got != ScopeOnce {
			t.Errorf("scopeForAnswer(%q) = %q, want once — an unrecognised answer must never widen", answer, got)
		}
	}
}

// TestScopeLabelsNameTheirSubject proves the operator reads WHAT they are widening. A label
// that said only "Always approve" would be a consent they could not scope, which is the
// failure this amendment exists to avoid.
func TestScopeLabelsNameTheirSubject(t *testing.T) {
	t.Parallel()
	subject := grantSubject{Tool: "calendar", Action: "delete_event"}
	if subject.String() != "calendar delete_event" {
		t.Fatalf("subject.String() = %q", subject.String())
	}
	for _, e := range scopeLabels(subject) {
		if e.Scope == ScopeOnce {
			continue // "Approve once" is about THIS call; naming the verb adds nothing
		}
		if !strings.Contains(e.Label, "calendar delete_event") {
			t.Errorf("%q scope label %q does not name its subject", e.Scope, e.Label)
		}
	}
	if plain := (grantSubject{Tool: "shell_exec"}).String(); plain != "shell_exec" {
		t.Fatalf("verb-less subject renders as %q, want the bare tool", plain)
	}
}
