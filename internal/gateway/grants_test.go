package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
)

const testIdentity = "00000000-0000-0000-0000-000000000001"

// fakeGrants is an in-memory grantStore. grantErr/hasErr force the two failure paths that
// must fall back rather than fail the turn.
type fakeGrants struct {
	mu       sync.Mutex
	rows     map[string]string // "tool\x00action" -> grantedBy
	grantErr error
	hasErr   error
	granted  int
}

func newFakeGrants() *fakeGrants { return &fakeGrants{rows: map[string]string{}} }

func (f *fakeGrants) Grant(_ context.Context, _, tool, action, grantedBy string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.grantErr != nil {
		return f.grantErr
	}
	f.granted++
	f.rows[tool+"\x00"+action] = grantedBy
	return nil
}

func (f *fakeGrants) Has(_ context.Context, _, tool, action string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.hasErr != nil {
		return false, f.hasErr
	}
	_, ok := f.rows[tool+"\x00"+action]
	return ok, nil
}

// approvedGateway is the hardened gateway an operator has a live session with.
func approvedGateway() (*Gateway, *fakeStore, context.Context) {
	store := &fakeStore{}
	g := New(config.ProfileSingleUserHardened, store)
	return g, store, identityctx.WithIdentityID(WithResponder(context.Background()), testIdentity)
}

// answerFor is the label a button press would send for a scope, taken from the same table
// the gateway generated the options from.
func answerFor(args json.RawMessage, scope ApprovalScope) string {
	for _, e := range scopeLabels(subjectFor(gatedSpec(), args)) {
		if e.Scope == scope {
			return e.Label
		}
	}
	return ""
}

// withhold drives the first Decide and returns the operator-visible question, failing the
// test unless the call was actually withheld.
func withhold(t *testing.T, g *Gateway, ctx context.Context, args json.RawMessage) string {
	t.Helper()
	v, err := g.Decide(ctx, gatedSpec(), args, testKey())
	if err != nil || v.Decision != Approve || v.ApprovalRequest == nil {
		t.Fatalf("first Decide = %+v, err %v — want a withheld approve", v, err)
	}
	var payload struct {
		Question string `json:"question"`
	}
	if err := json.Unmarshal([]byte(v.ApprovalRequest.Preview), &payload); err != nil {
		t.Fatalf("approval payload: %v", err)
	}
	return payload.Question
}

// accept completes the operator round trip and returns the scope that took effect.
func accept(t *testing.T, g *Gateway, ctx context.Context, args json.RawMessage, answer string) ApprovalScope {
	t.Helper()
	scope, err := g.ApproveChallenge(ctx, ApprovalAccept{
		ConversationID:  testKey().ConversationID,
		Tool:            gatedSpec().Name,
		ArgsFingerprint: gatewayArgsFingerprint(args),
		Question:        withhold(t, g, ctx, args),
		Answer:          answer,
		IdentityID:      identityctx.IdentityID(ctx),
		OperatorID:      "local",
	})
	if err != nil {
		t.Fatalf("ApproveChallenge: %v", err)
	}
	return scope
}

// TestSessionScopeCoversTheRestOfTheConversation is the measured friction amendment #127
// removes: with ScopeOnce the SECOND delete is withheld again, and with ScopeSession it is
// not — including for a DIFFERENT object, which a one-shot approval could never cover
// because its key carries the args fingerprint.
func TestSessionScopeCoversTheRestOfTheConversation(t *testing.T) {
	t.Parallel()
	first := json.RawMessage(`{"action":"delete","name":"probe-1"}`)
	second := json.RawMessage(`{"action":"delete","name":"probe-2"}`)

	t.Run("once leaves the next call withheld", func(t *testing.T) {
		t.Parallel()
		g, _, ctx := approvedGateway()
		if got := accept(t, g, ctx, first, answerFor(first, ScopeOnce)); got != ScopeOnce {
			t.Fatalf("scope = %q, want once", got)
		}
		v, err := g.Decide(ctx, gatedSpec(), second, testKey())
		if err != nil {
			t.Fatalf("Decide: %v", err)
		}
		if v.Decision != Approve {
			t.Fatalf("second delete = %q, want approve — a one-shot must not cover another object", v.Decision)
		}
	})

	t.Run("session covers the next call", func(t *testing.T) {
		t.Parallel()
		g, store, ctx := approvedGateway()
		if got := accept(t, g, ctx, first, answerFor(first, ScopeSession)); got != ScopeSession {
			t.Fatalf("scope = %q, want session", got)
		}
		v, err := g.Decide(ctx, gatedSpec(), second, testKey())
		if err != nil {
			t.Fatalf("Decide: %v", err)
		}
		if v.Decision != Allow {
			t.Fatalf("second delete = %q, want allow under a session grant", v.Decision)
		}
		// The audit trail must say WHY an unprompted destructive call ran.
		assertReservedScope(t, store, string(ScopeSession))
	})
}

// TestSessionScopeDiesWithItsConversation proves the session grant expires with the
// conversation — the bound that makes it safe to offer.
func TestSessionScopeDiesWithItsConversation(t *testing.T) {
	t.Parallel()
	args := json.RawMessage(`{"action":"delete","name":"probe"}`)
	g, _, ctx := approvedGateway()
	accept(t, g, ctx, args, answerFor(args, ScopeSession))

	g.EvictSession(testKey().ConversationID)

	v, err := g.Decide(ctx, gatedSpec(), args, testKey())
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if v.Decision != Approve {
		t.Fatalf("after eviction = %q, want approve — a session grant must not outlive its conversation", v.Decision)
	}
}

// TestAlwaysScopePersistsAndIsSubjectScoped proves the durable grant is written, is honoured
// by a gateway with an empty memory, and does NOT leak to a sibling verb of the same tool.
func TestAlwaysScopePersistsAndIsSubjectScoped(t *testing.T) {
	t.Parallel()
	args := json.RawMessage(`{"action":"delete","name":"probe"}`)
	grants := newFakeGrants()
	g, _, ctx := approvedGateway()
	g.SetGrantStore(grants)

	if got := accept(t, g, ctx, args, answerFor(args, ScopeAlways)); got != ScopeAlways {
		t.Fatalf("scope = %q, want always", got)
	}
	if grants.granted != 1 {
		t.Fatalf("durable grants written = %d, want 1", grants.granted)
	}

	// A FRESH gateway over the same durable store: nothing in memory, everything in the row.
	fresh := New(config.ProfileSingleUserHardened, &fakeStore{})
	fresh.SetGrantStore(grants)
	v, err := fresh.Decide(ctx, gatedSpec(), args, testKey())
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if v.Decision != Allow || v.Scope != ScopeAlways {
		t.Fatalf("verdict = %+v, want allow/always across a restart", v)
	}

}

// TestAlwaysGrantDoesNotLeakToASiblingVerb is the amendment's own example, run: the curated
// calendar tool multiplexes delete_event AND send_email, both destructive. A grant on one
// must leave the other withheld, or "always approve" on a deletion quietly authorizes mail.
func TestAlwaysGrantDoesNotLeakToASiblingVerb(t *testing.T) {
	t.Parallel()
	del := json.RawMessage(`{"action":"delete_event","eventId":"e-1"}`)
	mail := json.RawMessage(`{"action":"send_email","to":"a@b.c"}`)
	grants := newFakeGrants()
	g, _, ctx := approvedGateway()
	g.SetGrantStore(grants)

	question := calendarWithhold(t, g, ctx, del)
	scope, err := g.ApproveChallenge(ctx, ApprovalAccept{
		ConversationID:  testKey().ConversationID,
		Tool:            calendarSpec().Name,
		ArgsFingerprint: gatewayArgsFingerprint(del),
		Question:        question,
		Answer:          calendarAnswer(del, ScopeAlways),
		IdentityID:      testIdentity,
		OperatorID:      "local",
	})
	if err != nil || scope != ScopeAlways {
		t.Fatalf("ApproveChallenge = %q, err %v", scope, err)
	}

	granted, err := g.Decide(ctx, calendarSpec(), json.RawMessage(`{"action":"delete_event","eventId":"e-2"}`), testKey())
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if granted.Decision != Allow {
		t.Fatalf("a second delete_event = %q, want allow under the grant", granted.Decision)
	}
	sibling, err := g.Decide(ctx, calendarSpec(), mail, testKey())
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if sibling.Decision != Approve {
		t.Fatalf("send_email = %q, want approve — a delete grant must not authorize mail", sibling.Decision)
	}
}

// calendarSpec is the curated multiplexed PIM tool: one name, fourteen verbs, several of
// them destructive — the shape the grant subject exists for.
func calendarSpec() tools.Spec {
	return tools.Spec{Name: mcptools.CalendarMultiplexedToolName, Mutating: true}
}

func calendarAnswer(args json.RawMessage, scope ApprovalScope) string {
	for _, e := range scopeLabels(subjectFor(calendarSpec(), args)) {
		if e.Scope == scope {
			return e.Label
		}
	}
	return ""
}

func calendarWithhold(t *testing.T, g *Gateway, ctx context.Context, args json.RawMessage) string {
	t.Helper()
	v, err := g.Decide(ctx, calendarSpec(), args, testKey())
	if err != nil || v.Decision != Approve || v.ApprovalRequest == nil {
		t.Fatalf("first Decide = %+v, err %v — want a withheld approve", v, err)
	}
	var payload struct {
		Question string `json:"question"`
	}
	if err := json.Unmarshal([]byte(v.ApprovalRequest.Preview), &payload); err != nil {
		t.Fatalf("approval payload: %v", err)
	}
	return payload.Question
}

// TestAlwaysDegradesRatherThanLying is the honesty property: when the widest scope cannot be
// persisted, the operator gets the next-widest one they CAN be given — never "always"
// reported over a failed INSERT, and never a silent nothing.
func TestAlwaysDegradesRatherThanLying(t *testing.T) {
	t.Parallel()
	args := json.RawMessage(`{"action":"delete","name":"probe"}`)
	always := answerFor(args, ScopeAlways)

	cases := []struct {
		name    string
		store   *fakeGrants
		unnamed bool // drop the authenticated identity
	}{
		{name: "no durable store"},
		{name: "write fails", store: &fakeGrants{rows: map[string]string{}, grantErr: errors.New("boom")}},
		{name: "no authenticated identity", store: newFakeGrants(), unnamed: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g, _, ctx := approvedGateway()
			if tc.store != nil {
				g.SetGrantStore(tc.store)
			}
			if tc.unnamed {
				ctx = WithResponder(context.Background())
			}
			scope, err := g.ApproveChallenge(ctx, ApprovalAccept{
				ConversationID:  testKey().ConversationID,
				Tool:            gatedSpec().Name,
				ArgsFingerprint: gatewayArgsFingerprint(args),
				Question:        withhold(t, g, ctx, args),
				Answer:          always,
				IdentityID:      identityctx.IdentityID(ctx),
				OperatorID:      "local",
			})
			if err != nil {
				t.Fatalf("ApproveChallenge: %v", err)
			}
			if scope != ScopeSession {
				t.Fatalf("scope = %q, want session — an unpersistable always must degrade, not lie", scope)
			}
			// And the degraded grant must actually WORK for this conversation, or the
			// operator paid for a widening they did not get.
			next, err := g.Decide(ctx, gatedSpec(), json.RawMessage(`{"action":"delete","name":"other"}`), testKey())
			if err != nil {
				t.Fatalf("Decide: %v", err)
			}
			if next.Decision != Allow {
				t.Fatalf("degraded grant did not cover the conversation: %q", next.Decision)
			}
		})
	}
}

// TestGrantLookupErrorFallsThroughToTheApproval proves a policy READ failing fails to the
// PROMPT, never to an allow and never to a broken turn.
func TestGrantLookupErrorFallsThroughToTheApproval(t *testing.T) {
	t.Parallel()
	grants := newFakeGrants()
	grants.hasErr = errors.New("postgres is down")
	g, _, ctx := approvedGateway()
	g.SetGrantStore(grants)

	v, err := g.Decide(ctx, gatedSpec(), json.RawMessage(`{"action":"delete","name":"x"}`), testKey())
	if err != nil {
		t.Fatalf("a grant lookup failure must not fail the turn: %v", err)
	}
	if v.Decision != Approve {
		t.Fatalf("decision = %q, want approve — a failed policy read must fall through to the prompt", v.Decision)
	}
}

// TestApprovalRequestCarriesTheScopeOptions proves the withheld result hands the model the
// three server-generated labels: the operator can only choose from what is relayed.
func TestApprovalRequestCarriesTheScopeOptions(t *testing.T) {
	t.Parallel()
	args := json.RawMessage(`{"action":"delete","name":"x"}`)
	g, _, ctx := approvedGateway()

	v, err := g.Decide(ctx, gatedSpec(), args, testKey())
	if err != nil || v.ApprovalRequest == nil {
		t.Fatalf("Decide = %+v, err %v", v, err)
	}
	var payload struct {
		Options []string `json:"options"`
		Message string   `json:"message"`
	}
	if err := json.Unmarshal([]byte(v.ApprovalRequest.Preview), &payload); err != nil {
		t.Fatalf("approval payload: %v", err)
	}
	want := scopeOptionLabels(subjectFor(gatedSpec(), args))
	if len(payload.Options) != len(want) {
		t.Fatalf("options = %v, want %v", payload.Options, want)
	}
	for i, w := range want {
		if payload.Options[i] != w {
			t.Fatalf("option[%d] = %q, want %q", i, payload.Options[i], w)
		}
	}
	if !strings.Contains(payload.Message, "options exactly equal to the options field") {
		t.Fatalf("the message does not tell the model to relay the options: %q", payload.Message)
	}
}

// TestProductionRefusesEveryScope is WR-01 defense-in-depth carried onto the new surface: a
// production run records no approval by any path, so it can grant no scope either.
func TestProductionRefusesEveryScope(t *testing.T) {
	t.Parallel()
	args := json.RawMessage(`{"action":"delete","name":"x"}`)
	grants := newFakeGrants()
	g := New(config.ProfileServerProduction, &fakeStore{})
	g.SetGrantStore(grants)

	scope, err := g.ApproveChallenge(context.Background(), ApprovalAccept{
		ConversationID:  testKey().ConversationID,
		Tool:            gatedSpec().Name,
		ArgsFingerprint: gatewayArgsFingerprint(args),
		Question:        "anything",
		Answer:          answerFor(args, ScopeAlways),
		IdentityID:      testIdentity,
		OperatorID:      "local",
	})
	if err == nil {
		t.Fatal("production accepted an approval")
	}
	if scope != ScopeOnce {
		t.Fatalf("scope = %q, want once on the refusal path", scope)
	}
	if grants.granted != 0 {
		t.Fatalf("production wrote %d durable grant(s)", grants.granted)
	}
}

// assertReservedScope checks the reservation start recorded which standing grant let the
// call through.
func assertReservedScope(t *testing.T, store *fakeStore, want string) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, e := range store.reserved {
		if e.Meta["approval_scope"] == want {
			return
		}
	}
	t.Fatalf("no reservation carries approval_scope=%q: %+v", want, store.reserved)
}
