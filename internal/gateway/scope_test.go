package gateway

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
)

// TestSubjectForReadsTheVerbOfAMultiplexedToolOnly proves the grant key is the tool for an
// ordinary tool and tool+verb for an action-multiplexed one — the property amendment #127
// rests on, because a subject that dropped the verb would make "always approve task
// cancel" silently authorize "task run_now" too.
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
		{"task carries its verb",
			tools.Spec{Name: "task"}, `{"action":"cancel","id":"t-1"}`,
			grantSubject{Tool: "task", Action: "cancel"}},
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

// TestCalendarSubjectIsTheToolAlone: each PIM operation is its own tool, so the tool name
// is the whole subject. An `action` argument left over from the multiplexed shape is the
// model's text, not a verb, and must not narrow or widen it.
func TestCalendarSubjectIsTheToolAlone(t *testing.T) {
	t.Parallel()
	spec := tools.Spec{Name: "calendar__send_email"}
	for _, args := range []string{`{"to":["a@b.c"],"subject":"s"}`, `{"action":"delete_event","to":["a@b.c"]}`} {
		if got := subjectFor(spec, json.RawMessage(args)); got != (grantSubject{Tool: "calendar__send_email"}) {
			t.Fatalf("subjectFor(%s) = %+v, want the tool alone", args, got)
		}
	}
}

// TestAliasedCalendarToolKeepsItsOwnSubject: a PIM mounted under another namespace keys its
// grants on its own registered name, the same rule as under the built-in one.
func TestAliasedCalendarToolKeepsItsOwnSubject(t *testing.T) {
	t.Parallel()
	got := subjectFor(tools.Spec{Name: "work__send_email"}, json.RawMessage(`{"to":["a@b.c"]}`))
	if got != (grantSubject{Tool: "work__send_email"}) {
		t.Fatalf("subjectFor = %+v, want the aliased tool alone", got)
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
		{Tool: "calendar__delete_event"},
	} {
		opts := scopeOptions(subject, "")
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
	opts := scopeOptions(grantSubject{Tool: "task", Action: "cancel"}, "")
	want := []string{
		"gateway_scope:once:task cancel",
		"gateway_scope:session:task cancel",
		"gateway_scope:always:task cancel",
	}
	for i, w := range want {
		if opts[i].Value != w {
			t.Errorf("option[%d].Value = %q, want %q", i, opts[i].Value, w)
		}
	}
	// A subject with no verb still yields a two-colon shape the surfaces can split.
	if got := scopeOptions(grantSubject{Tool: "shell_exec"}, "")[2].Value; got != "gateway_scope:always:shell_exec" {
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
	other := grantSubject{Tool: "task", Action: "cancel"}

	for _, e := range scopeLabels(subject, "") {
		// Both the stable code the surfaces submit and the English fallback resolve.
		for _, answer := range []string{scopeOptionValue(e.Scope, subject), e.Label} {
			if got := scopeForAnswer(subject, "", answer); got != e.Scope {
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
	for _, e := range scopeLabels(other, "") {
		// Another subject's labels AND its codes grant nothing here: an answer lifted from
		// a different approval must not widen this one.
		widening = append(widening, e.Label, scopeOptionValue(e.Scope, other))
	}
	for _, answer := range widening {
		if got := scopeForAnswer(subject, "", answer); got != ScopeOnce {
			t.Errorf("scopeForAnswer(%q) = %q, want once — an unrecognised answer must never widen", answer, got)
		}
	}
}

// TestScopeLabelsNameTheirSubject proves the operator reads WHAT they are widening. A label
// that said only "Always approve" would be a consent they could not scope, which is the
// failure this amendment exists to avoid.
func TestScopeLabelsNameTheirSubject(t *testing.T) {
	t.Parallel()
	subject := grantSubject{Tool: "task", Action: "cancel"}
	if subject.String() != "task cancel" {
		t.Fatalf("subject.String() = %q", subject.String())
	}
	for _, e := range scopeLabels(subject, "") {
		if e.Scope == ScopeOnce {
			continue // "Approve once" is about THIS call; naming the verb adds nothing
		}
		if !strings.Contains(e.Label, "task cancel") {
			t.Errorf("%q scope label %q does not name its subject", e.Scope, e.Label)
		}
	}
	if plain := (grantSubject{Tool: "shell_exec"}).String(); plain != "shell_exec" {
		t.Fatalf("verb-less subject renders as %q, want the bare tool", plain)
	}
}

// The option labels and values are the operator's words for what a grant covers, built from
// the model's own action, so they pass through approvalLine (spec 2026-10-12, "One
// sanitizer"). The subject a policy or a grant is keyed on keeps the raw action: a stored
// policy or grant matches exactly the calls it matched before.
func TestScopeLabelsAreOneLineButGrantsKeepTheRawAction(t *testing.T) {
	t.Parallel()
	const action = "purge\n<all>"
	args := mustArgs(t, map[string]string{"action": action, "id": "c1"})
	// The ask policy is stored under the raw action; the call is withheld only if it matched.
	g, _, ctx := policyGateway(map[string]Policy{policyKey("board", action): PolicyAsk})
	v, err := g.Decide(ctx, boardSpec(), args, testKey())
	if err != nil || v.Decision != Approve || v.ApprovalRequest == nil {
		t.Fatalf("Decide = %+v, %v; want the ask policy to withhold the call", v, err)
	}
	var payload struct {
		Question string        `json:"question"`
		Options  []ScopeOption `json:"options"`
	}
	if err := json.Unmarshal([]byte(v.ApprovalRequest.Preview), &payload); err != nil {
		t.Fatalf("approval payload: %v", err)
	}
	want := []ScopeOption{
		{Label: "Approve once", Value: "gateway_scope:once:board purge all"},
		{Label: "Approve board purge all for this conversation", Value: "gateway_scope:session:board purge all"},
	}
	if !slices.Equal(payload.Options, want) {
		t.Fatalf("options = %+v, want one-line labels %+v", payload.Options, want)
	}
	scope, err := g.ApproveChallenge(ctx, ApprovalAccept{
		ConversationID: testKey().ConversationID, Tool: "board", ArgsFingerprint: gatewayArgsFingerprint(args),
		Question: payload.Question, Answer: want[1].Value, IdentityID: testIdentity, OperatorID: "local",
	})
	if err != nil || scope != ScopeSession {
		t.Fatalf("ApproveChallenge = %q, %v; want the session scope the operator pressed", scope, err)
	}
	if _, ok := g.approvals.SessionGrant(testKey().ConversationID, grantSubject{Tool: "board", Action: action}); !ok {
		t.Fatal("the session grant is not keyed on the raw action")
	}
	next := testKey()
	next.ToolCallID = "call-2"
	if v, err := g.Decide(ctx, boardSpec(), args, next); err != nil || v.Decision != Allow || v.Scope != ScopeSession {
		t.Fatalf("second Decide = %+v, %v; want the session grant to cover the same raw call", v, err)
	}
}
