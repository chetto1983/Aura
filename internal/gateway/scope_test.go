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

// TestScopeOptionsAreRelayable proves the options the model is told to copy verbatim are a
// payload ask_user accepts: 2-4 entries, non-empty and distinct on BOTH coordinates. A set
// that failed that validation would surface as a tool error with the destructive call
// already withheld and no way left for the operator to answer.
func TestScopeOptionsAreRelayable(t *testing.T) {
	t.Parallel()
	for _, subject := range []grantSubject{
		{Tool: "shell_exec"},
		{Tool: "skill_manage", Action: "delete"},
		{Tool: mcptools.CalendarMultiplexedToolName, Action: "delete_event"},
	} {
		opts := scopeOptions(subject)
		if len(opts) < 2 || len(opts) > 4 {
			t.Fatalf("subject %q: %d options, ask_user accepts 2-4", subject, len(opts))
		}
		labels, values := map[string]bool{}, map[string]bool{}
		for _, o := range opts {
			if o.Label == "" || labels[o.Label] {
				t.Fatalf("subject %q: label %q is empty or repeated", subject, o.Label)
			}
			if o.Value == "" || values[o.Value] {
				t.Fatalf("subject %q: value %q is empty or repeated", subject, o.Value)
			}
			labels[o.Label], values[o.Value] = true, true
		}
	}
}

// TestScopeOptionValueIsALocaleFreeCode pins the wire contract the cockpit and Telegram
// decode: a stable prefix, the scope, and the subject, with no display text in it. Both
// surfaces render their own words from this, so a change here is a change to their copy.
func TestScopeOptionValueIsALocaleFreeCode(t *testing.T) {
	t.Parallel()
	opts := scopeOptions(grantSubject{Tool: "calendar", Action: "delete_event"})
	want := []string{
		"gateway_scope:once:calendar delete_event",
		"gateway_scope:session:calendar delete_event",
		"gateway_scope:always:calendar delete_event",
	}
	for i, w := range want {
		if opts[i].Value != w {
			t.Errorf("option[%d].Value = %q, want %q", i, opts[i].Value, w)
		}
	}
	// A subject with no verb still yields a two-colon shape the surfaces can split.
	if got := scopeOptions(grantSubject{Tool: "shell_exec"})[2].Value; got != "gateway_scope:always:shell_exec" {
		t.Errorf("verb-less always value = %q", got)
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
		// Both the stable code the surfaces submit and the English fallback resolve.
		for _, answer := range []string{scopeOptionValue(e.Scope, subject), e.Label} {
			if got := scopeForAnswer(subject, answer); got != e.Scope {
				t.Errorf("scopeForAnswer(%q) = %q, want %q", answer, got, e.Scope)
			}
		}
	}
	widening := []string{
		"", "approvo", "yes",
		"gateway_scope:always",                         // truncated code, no subject
		"gateway_scope:always:",                        // empty subject
		"gateway_scope:everything:skill_manage delete", // unknown scope
		"gateway_scope:always:skill_manage",            // the tool without its verb
		"Approve once ",                                // trailing space — not the label
		"approve once",                                 // case-shifted — not the label
		"Always approve everything",
		"Approve skill_manage for this convo", // reworded
	}
	for _, e := range scopeLabels(other) {
		// Another subject's labels AND its codes grant nothing here: an answer lifted from
		// a different approval must not widen this one.
		widening = append(widening, e.Label, scopeOptionValue(e.Scope, other))
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
