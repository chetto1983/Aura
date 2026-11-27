package gateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/approvaltext"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/scoring"
)

var obsoleteSkillArgs = json.RawMessage(`{"action":"delete","name":"obsolete-skill"}`)

// withApprovedCall is the prelude's dispatch context for an accepted call: the tool-call
// context of the reservation key and one ApprovedCall over args.
func withApprovedCall(ctx context.Context, key ReservationKey, tool string, args json.RawMessage, operator string, scope ApprovalScope) context.Context {
	call := tools.ApprovedCall{
		ConversationID: key.ConversationID, ToolCallID: key.ToolCallID, ToolName: tool,
		Arguments: args, Fingerprint: tools.CallFingerprint(args), OperatorID: operator, Scope: string(scope),
	}
	ctx = tools.WithApprovedCalls(ctx, []tools.ApprovedCall{call})
	return tools.WithToolCallContext(ctx, key.ConversationID, key.ToolCallID, "", 0)
}

func assertReviewWroteNothing(t *testing.T, g *Gateway, store *fakeStore) {
	t.Helper()
	if n := len(store.calls()) + len(store.reserves()); n != 0 {
		t.Fatalf("Review wrote %d ledger rows, want none", n)
	}
	if n := g.approvals.PendingCountForTool(testKey().ConversationID, gatedSpec().Name); n != 0 {
		t.Fatalf("Review left %d challenges, want none", n)
	}
}

func TestReviewNeedsADestructiveCallWithAPerson(t *testing.T) {
	t.Parallel()
	g, store, ctx := approvedGateway()
	describer := &countingDescriber{target: "Skill obsoleta · non usata"}

	r, err := g.Review(ctx, gatedSpec(), obsoleteSkillArgs, testKey(), describer)
	if err != nil || !r.Needed || r.Tier != scoring.Destructive {
		t.Fatalf("Review = %+v, %v; want a needed destructive review", r, err)
	}
	if r.SubjectTool != "skill_manage" || r.SubjectAction != "delete" {
		t.Fatalf("subject = %q %q, want skill_manage delete", r.SubjectTool, r.SubjectAction)
	}
	if len(r.Options) != 3 || r.Options[2].Value != scopeOptionValue(ScopeAlways, subjectFor(gatedSpec(), obsoleteSkillArgs)) {
		t.Fatalf("options = %+v, want once, session and always", r.Options)
	}
	if r.Question != gatewayApprovalQuestion(gatedSpec(), scoring.Destructive, obsoleteSkillArgs) {
		t.Fatalf("question = %q, want the gateway's own", r.Question)
	}
	p := r.Presentation
	const fallback = "not rendered"
	if p.Key != approvaltext.GatewayKey || approvaltext.RenderItalian(p, fallback) == fallback {
		t.Fatalf("presentation %+v is not a valid gateway card", p)
	}
	if p.Params["args"] != "action, name" || strings.Contains(p.Params["args"], "obsolete-skill") {
		t.Fatalf("args = %q, want the keys only", p.Params["args"])
	}
	if p.Params["target"] != describer.target || describer.calls != 1 {
		t.Fatalf("target = %q after %d describes, want the describer's once", p.Params["target"], describer.calls)
	}
	assertReviewWroteNothing(t, g, store)
}

func TestReviewUnderAskPolicyOffersNoAlways(t *testing.T) {
	t.Parallel()
	g, _, ctx := policyGateway(map[string]Policy{policyKey("read_file", ""): PolicyAsk})

	r, err := g.Review(ctx, readOnlySpec(), json.RawMessage(`{"path":"notes.md"}`), testKey(), nil)
	if err != nil || !r.Needed {
		t.Fatalf("Review = %+v, %v; want an ask policy to need the operator", r, err)
	}
	if len(r.Options) != 2 {
		t.Fatalf("options = %+v, want once and session only", r.Options)
	}
	for _, o := range r.Options {
		if strings.HasPrefix(o.Value, scopeOptionPrefix+string(ScopeAlways)) {
			t.Fatalf("an ask policy offered %q", o.Value)
		}
	}
}

func TestReviewIsNotNeededWhen(t *testing.T) {
	t.Parallel()
	key := testKey()
	approved := ResolvedApproval{Approved: true, OperatorID: "op-session"}
	cases := []struct {
		name  string
		build func() (*Gateway, context.Context)
		spec  tools.Spec
		args  json.RawMessage
	}{
		{"dev", func() (*Gateway, context.Context) {
			return New(config.ProfileDev, &fakeStore{}), WithResponder(context.Background())
		}, gatedSpec(), obsoleteSkillArgs},
		{"server_production", func() (*Gateway, context.Context) {
			g := New(config.ProfileServerProduction, &fakeStore{})
			return g, identityctx.WithIdentityID(WithResponder(context.Background()), testIdentity)
		}, gatedSpec(), obsoleteSkillArgs},
		{"deny policy", func() (*Gateway, context.Context) {
			g, _, ctx := policyGateway(map[string]Policy{policyKey("skill_manage", "delete"): PolicyDeny})
			return g, ctx
		}, gatedSpec(), obsoleteSkillArgs},
		{"session grant", func() (*Gateway, context.Context) {
			g, _, ctx := approvedGateway()
			g.approvals.GrantSession(key.ConversationID, subjectFor(gatedSpec(), obsoleteSkillArgs), approved)
			return g, ctx
		}, gatedSpec(), obsoleteSkillArgs},
		{"always grant", func() (*Gateway, context.Context) {
			g, _, ctx := approvedGateway()
			grants := newFakeGrants()
			grants.rows[policyKey("skill_manage", "delete")] = testIdentity
			g.SetGrantStore(grants)
			return g, ctx
		}, gatedSpec(), obsoleteSkillArgs},
		{"no responder", func() (*Gateway, context.Context) {
			g := New(config.ProfileSingleUserHardened, &fakeStore{})
			return g, identityctx.WithIdentityID(context.Background(), testIdentity)
		}, gatedSpec(), obsoleteSkillArgs},
		{"delegated", func() (*Gateway, context.Context) {
			g, _, ctx := approvedGateway()
			return g, WithDelegatedDispatch(ctx)
		}, gatedSpec(), obsoleteSkillArgs},
		{"Normal tier", func() (*Gateway, context.Context) {
			g, _, ctx := approvedGateway()
			return g, ctx
		}, taskSpec(), json.RawMessage(`{"action":"cancel","task_id":"7"}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g, ctx := tc.build()
			describer := &countingDescriber{target: "never asked"}
			r, err := g.Review(ctx, tc.spec, tc.args, key, describer)
			if err != nil || r.Needed {
				t.Fatalf("Review = %+v, %v; want not needed", r, err)
			}
			if describer.calls != 0 {
				t.Fatal("a call that needs no review was described")
			}
		})
	}
}

func TestReviewWritesNothing(t *testing.T) {
	t.Parallel()
	g, store, ctx := approvedGateway()
	operations := acquiringOperations()
	g.SetOperationRegistry(operations)
	grants := newFakeGrants()
	g.SetGrantStore(grants)

	for range 2 {
		if r, err := g.Review(ctx, gatedSpec(), obsoleteSkillArgs, testKey(), nil); err != nil || !r.Needed {
			t.Fatalf("Review = %+v, %v; want needed", r, err)
		}
	}
	assertReviewWroteNothing(t, g, store)
	if len(operations.begins) != 0 || grants.granted != 0 {
		t.Fatalf("Review began %d operations and wrote %d grants, want none", len(operations.begins), grants.granted)
	}
	if _, ok := g.approvals.SessionGrant(testKey().ConversationID, subjectFor(gatedSpec(), obsoleteSkillArgs)); ok {
		t.Fatal("Review recorded a session grant")
	}
}

func TestDecideHonoursAnApprovedCall(t *testing.T) {
	t.Parallel()
	g, store, ctx := approvedGateway()
	key := testKey()
	ctx = withApprovedCall(ctx, key, gatedSpec().Name, obsoleteSkillArgs, "op-resolver", ScopeOnce)

	v, err := g.Decide(ctx, gatedSpec(), json.RawMessage(`{"name":"obsolete-skill","action":"delete"}`), key)
	if err != nil || v.Decision != Allow || v.OperatorID != "op-resolver" {
		t.Fatalf("Decide = %+v, %v; want the approved call allowed for its operator", v, err)
	}
	reserved := store.reserves()
	if len(reserved) != 1 || reserved[0].Meta["operator_id"] != "op-resolver" || reserved[0].Meta["approval_scope"] != "once" {
		t.Fatalf("reservations = %+v, want one with operator_id and approval_scope", reserved)
	}
	if g.approvals.PendingCountForTool(key.ConversationID, gatedSpec().Name) != 0 {
		t.Fatal("an approved call was challenged again")
	}
}

func TestApprovedNormalTierCallReservesWithTheOperator(t *testing.T) {
	t.Parallel()
	args := json.RawMessage(`{"action":"cancel","task_id":"7"}`)
	key := testKey()

	g, store, ctx := approvedGateway()
	v, err := g.Decide(withApprovedCall(ctx, key, "task", args, "op-resolver", ScopeOnce), taskSpec(), args, key)
	if err != nil || v.Decision != Allow || v.Tier != scoring.Normal {
		t.Fatalf("Decide = %+v, %v; want a Normal-tier allow", v, err)
	}
	reserved := store.reserves()
	if len(reserved) != 1 || reserved[0].Meta["operator_id"] != "op-resolver" || reserved[0].Meta["approval_scope"] != "once" {
		t.Fatalf("reservations = %+v, want one naming the operator and scope", reserved)
	}

	g, store, ctx = approvedGateway()
	if v, err := g.Decide(ctx, taskSpec(), args, key); err != nil || v.Decision != Allow || v.OperatorID != "" {
		t.Fatalf("unapproved Decide = %+v, %v; want a plain allow", v, err)
	}
	reserved = store.reserves()
	if len(reserved) != 1 {
		t.Fatalf("reservations = %+v, want one", reserved)
	}
	_, hasOperator := reserved[0].Meta["operator_id"]
	_, hasScope := reserved[0].Meta["approval_scope"]
	if hasOperator || hasScope {
		t.Fatalf("reservations = %+v, want one with neither operator nor scope", reserved)
	}
}

func TestAuthorizationNeverAllowsADenyPolicy(t *testing.T) {
	t.Parallel()
	args := json.RawMessage(`{"action":"cancel","task_id":"7"}`)
	key := testKey()
	g, store, ctx := policyGateway(map[string]Policy{policyKey("task", "cancel"): PolicyDeny})

	v, err := g.Decide(withApprovedCall(ctx, key, "task", args, "op-resolver", ScopeOnce), taskSpec(), args, key)
	if err != nil || v.Decision != Deny || v.Reason != policyDenyReason(grantSubject{Tool: "task", Action: "cancel"}) {
		t.Fatalf("Decide = %+v, %v; want the policy's deny", v, err)
	}
	assertDenyFactReason(t, store, "policy")
}

func TestDecideRefusesAnApprovedCallWithOtherArgs(t *testing.T) {
	t.Parallel()
	g, store, ctx := approvedGateway()
	key := testKey()
	ctx = withApprovedCall(ctx, key, gatedSpec().Name, obsoleteSkillArgs, "op-resolver", ScopeOnce)

	v, err := g.Decide(ctx, gatedSpec(), json.RawMessage(`{"action":"delete","name":"live-skill"}`), key)
	if err != nil || v.Decision == Allow {
		t.Fatalf("Decide = %+v, %v; other arguments must not run on the accept", v, err)
	}
	if n := len(store.reserves()); n != 0 {
		t.Fatalf("%d reservations for a call nobody approved", n)
	}
}

func TestDenyPolicyAddedAfterTheParkStillRefuses(t *testing.T) {
	t.Parallel()
	g, store, ctx := approvedGateway()
	key := testKey()
	if r, err := g.Review(ctx, gatedSpec(), obsoleteSkillArgs, key, nil); err != nil || !r.Needed {
		t.Fatalf("Review = %+v, %v; want the park", r, err)
	}
	g.SetPolicyStore(newFakePolicies(map[string]Policy{policyKey("skill_manage", "delete"): PolicyDeny}))

	v, err := g.Decide(withApprovedCall(ctx, key, gatedSpec().Name, obsoleteSkillArgs, "op-resolver", ScopeOnce), gatedSpec(), obsoleteSkillArgs, key)
	if err != nil || v.Decision != Deny || !v.Refusal {
		t.Fatalf("Decide = %+v, %v; want the new policy's deny", v, err)
	}
	if n := len(store.reserves()); n != 0 {
		t.Fatalf("%d reservations under a deny policy", n)
	}
	assertDenyFactReason(t, store, "policy")
}

func TestApprovedCallUnderProductionIsDenied(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	g := New(config.ProfileServerProduction, store)
	operations := acquiringOperations()
	g.SetOperationRegistry(operations)
	key := testKey()
	ctx := identityctx.WithIdentityID(WithResponder(context.Background()), testIdentity)
	ctx = withApprovedCall(ctx, key, gatedSpec().Name, obsoleteSkillArgs, "op-resolver", ScopeOnce)

	v, err := g.Decide(ctx, gatedSpec(), obsoleteSkillArgs, key)
	if err != nil || v.Decision != Deny || !strings.Contains(v.Reason, "no interactive approver") {
		t.Fatalf("Decide = %+v, %v; want the no-approver deny", v, err)
	}
	if len(store.reserves()) != 0 || len(operations.begins) != 0 {
		t.Fatal("a production approved call reached the reservation")
	}
	assertDenyFactReason(t, store, "no_approver")
}

func TestHeadlessWithAnAlwaysGrantStaysDenied(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	g := New(config.ProfileSingleUserHardened, store)
	grants := newFakeGrants()
	grants.rows[policyKey("skill_manage", "delete")] = testIdentity
	g.SetGrantStore(grants)
	ctx := identityctx.WithIdentityID(context.Background(), testIdentity)

	v, err := g.Decide(ctx, gatedSpec(), obsoleteSkillArgs, testKey())
	if err != nil || v.Decision != Deny || !strings.Contains(v.Reason, "no interactive approver") {
		t.Fatalf("Decide = %+v, %v; want the no-approver deny", v, err)
	}
	if len(store.reserves()) != 0 {
		t.Fatal("an always grant let a headless run through")
	}
	assertDenyFactReason(t, store, "no_approver")
}

func TestSyncWorkerWithASessionGrantIsAllowed(t *testing.T) {
	t.Parallel()
	g, store, ctx := approvedGateway()
	key := testKey()
	g.approvals.GrantSession(key.ConversationID, subjectFor(gatedSpec(), obsoleteSkillArgs),
		ResolvedApproval{Approved: true, OperatorID: "op-session"})

	v, err := g.Decide(WithDelegatedDispatch(ctx), gatedSpec(), obsoleteSkillArgs, key)
	if err != nil || v.Decision != Allow || v.OperatorID != "op-session" || v.Scope != ScopeSession {
		t.Fatalf("Decide = %+v, %v; want the session grant to cover the worker", v, err)
	}
	if n := len(store.reserves()); n != 1 {
		t.Fatalf("%d reservations, want one", n)
	}
}

func TestSyncWorkerWithoutAGrantIsDeniedNoApprover(t *testing.T) {
	t.Parallel()
	g, store, ctx := approvedGateway()

	v, err := g.Decide(WithDelegatedDispatch(ctx), gatedSpec(), obsoleteSkillArgs, testKey())
	if err != nil || v.Decision != Deny || v.ApprovalRequest != nil || !strings.Contains(v.Reason, "no interactive approver") {
		t.Fatalf("Decide = %+v, %v; want the no-approver deny, not a challenge", v, err)
	}
	if g.approvals.PendingCountForTool(testKey().ConversationID, gatedSpec().Name) != 0 {
		t.Fatal("a worker with nobody on it was challenged")
	}
	assertDenyFactReason(t, store, "no_approver")
}

func TestAlwaysGrantRecordsTheIdentityAsOperator(t *testing.T) {
	t.Parallel()
	g, store, ctx := approvedGateway()
	grants := newFakeGrants()
	grants.rows[policyKey("skill_manage", "delete")] = testIdentity
	g.SetGrantStore(grants)

	v, err := g.Decide(ctx, gatedSpec(), obsoleteSkillArgs, testKey())
	if err != nil || v.Decision != Allow || v.OperatorID != testIdentity || v.Scope != ScopeAlways {
		t.Fatalf("Decide = %+v, %v; want the always grant allowed for the identity", v, err)
	}
	reserved := store.reserves()
	if len(reserved) != 1 || reserved[0].Meta["operator_id"] != testIdentity || reserved[0].Meta["approval_scope"] != "always" {
		t.Fatalf("reservations = %+v, want one naming the identity", reserved)
	}
}

func TestScopeForAnswerReadsStoredOptions(t *testing.T) {
	t.Parallel()
	subject := grantSubject{Tool: "skill_manage", Action: "delete"}
	stored, err := json.Marshal(scopeOptions(subject, ""))
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range scopeOptions(subject, "") {
		want := scopeForAnswer(subject, "", o.Value)
		for _, answer := range []string{o.Value, o.Label} {
			if got := ScopeForAnswer(stored, answer); got != want {
				t.Errorf("ScopeForAnswer(%q) = %q, want %q", answer, got, want)
			}
		}
	}
	if got := ScopeForAnswer(stored, scopeOptionValue(ScopeAlways, grantSubject{Tool: "task", Action: "cancel"})); got != ScopeOnce {
		t.Errorf("another subject's always = %q, want once", got)
	}
	asked, _ := json.Marshal(scopeOptions(subject, PolicyAsk))
	if got := ScopeForAnswer(asked, scopeOptionValue(ScopeAlways, subject)); got != ScopeOnce {
		t.Errorf("always against an ask record = %q, want once", got)
	}
	forged, _ := json.Marshal([]ScopeOption{{Label: "Approve everything", Value: "gateway_scope:everything:skill_manage delete"}})
	for _, tc := range []struct {
		options json.RawMessage
		answer  string
	}{
		{stored, ""},
		{stored, "yes"},
		{json.RawMessage(`not json`), scopeOptionValue(ScopeAlways, subject)},
		{nil, scopeOptionValue(ScopeSession, subject)},
		{forged, "Approve everything"},
	} {
		if got := ScopeForAnswer(tc.options, tc.answer); got != ScopeOnce {
			t.Errorf("ScopeForAnswer(%s, %q) = %q, want once", tc.options, tc.answer, got)
		}
	}
}

func assertDenyFactReason(t *testing.T, store *fakeStore, reason string) {
	t.Helper()
	facts := store.calls()
	if len(facts) != 1 || facts[0].Meta["reason"] != reason {
		t.Fatalf("deny facts = %+v, want one with reason %q", facts, reason)
	}
}
