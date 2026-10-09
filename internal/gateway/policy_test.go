package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
)

// fakePolicies is an in-memory policyStore keyed like the table: "tool\x00action".
type fakePolicies struct {
	mu   sync.Mutex
	rows map[string]Policy
	err  error
}

func newFakePolicies(entries map[string]Policy) *fakePolicies {
	return &fakePolicies{rows: entries}
}

func (f *fakePolicies) Get(_ context.Context, _, tool, action string) (Policy, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return "", false, f.err
	}
	p, ok := f.rows[tool+"\x00"+action]
	return p, ok, nil
}

func policyKey(tool, action string) string { return tool + "\x00" + action }

// policyGateway is a hardened gateway with a live responder and the given policies.
func policyGateway(entries map[string]Policy) (*Gateway, *fakeStore, context.Context) {
	g, store, ctx := approvedGateway()
	g.SetPolicyStore(newFakePolicies(entries))
	return g, store, ctx
}

// A deny refuses before anything runs: no reservation, one terminal decision fact naming the
// policy, and a reason that tells the model not to retry.
func TestPolicyDenyRefusesBeforeExecution(t *testing.T) {
	t.Parallel()
	g, store, ctx := policyGateway(map[string]Policy{policyKey("shell_exec", ""): PolicyDeny})
	v, err := g.Decide(ctx, ordinaryWriteSpec(), json.RawMessage(`{"command":"ls"}`), testKey())
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if v.Decision != Deny {
		t.Fatalf("decision = %q, want deny", v.Decision)
	}
	if !strings.Contains(v.Reason, `"shell_exec"`) || !strings.Contains(v.Reason, "do not retry") {
		t.Errorf("reason %q must name the subject and say not to retry", v.Reason)
	}
	if n := len(store.reserves()); n != 0 {
		t.Fatalf("a denied call reserved %d slots, want 0", n)
	}
	facts := store.calls()
	if len(facts) != 1 || facts[0].Event != "end" || facts[0].Meta["gateway_policy"] != "deny" || facts[0].Meta["reason"] != "policy" {
		t.Fatalf("deny fact = %+v, want one end row marked gateway_policy=deny reason=policy", facts)
	}
	if facts[0].Meta["degraded_deny"] != nil {
		t.Error("a policy deny must not be recorded as a degraded deny")
	}
}

// A read-only tool is not exempt: deny is checked before the read-only early return.
func TestPolicyDenyCoversReadOnlyTools(t *testing.T) {
	t.Parallel()
	g, _, ctx := policyGateway(map[string]Policy{policyKey("read_file", ""): PolicyDeny})
	v, err := g.Decide(ctx, readOnlySpec(), json.RawMessage(`{"path":"x"}`), testKey())
	if err != nil || v.Decision != Deny {
		t.Fatalf("read-only under deny = %+v, %v; want deny", v, err)
	}
}

// A deny on one verb of a multiplexed tool leaves its siblings alone.
func TestPolicyDenyIsSubjectScoped(t *testing.T) {
	t.Parallel()
	g, _, ctx := policyGateway(map[string]Policy{policyKey("skill_manage", "delete"): PolicyDeny})
	if v, _ := g.Decide(ctx, gatedSpec(), gatedArgs(), testKey()); v.Decision != Deny {
		t.Fatalf("skill_manage delete = %q, want deny", v.Decision)
	}
	create := json.RawMessage(`{"action":"create","name":"x","description":"d","body":"b"}`)
	if v, _ := g.Decide(ctx, gatedSpec(), create, testKey()); v.Decision == Deny {
		t.Fatal("a deny on skill_manage delete must not refuse skill_manage create")
	}
}

// An ask turns a read-only call into an approved, reserved one, and the reservation says why.
func TestPolicyAskReservesAReadOnlyTool(t *testing.T) {
	t.Parallel()
	g, store, ctx := policyGateway(map[string]Policy{policyKey("read_file", ""): PolicyAsk})
	args := json.RawMessage(`{"path":"notes.md"}`)
	v, err := g.Decide(ctx, readOnlySpec(), args, testKey())
	if err != nil || v.Decision != Approve || v.ApprovalRequest == nil {
		t.Fatalf("first Decide = %+v, %v; want a withheld approve", v, err)
	}
	var payload struct {
		Question string `json:"question"`
	}
	if err := json.Unmarshal([]byte(v.ApprovalRequest.Preview), &payload); err != nil {
		t.Fatalf("approval payload: %v", err)
	}
	if _, err := g.ApproveChallenge(ctx, ApprovalAccept{
		ConversationID: testKey().ConversationID, Tool: "read_file",
		ArgsFingerprint: gatewayArgsFingerprint(args), Question: payload.Question,
		Answer: "Approve once", IdentityID: testIdentity, OperatorID: "local",
	}); err != nil {
		t.Fatalf("ApproveChallenge: %v", err)
	}
	v, err = g.Decide(ctx, readOnlySpec(), args, testKey())
	if err != nil || v.Decision != Allow {
		t.Fatalf("re-drive = %+v, %v; want allow", v, err)
	}
	reserved := store.reserves()
	if len(reserved) != 1 || reserved[0].Meta["gateway_policy"] != "ask" {
		t.Fatalf("reservations = %+v, want one marked gateway_policy=ask", reserved)
	}
}

// An ask outranks a standing "always" grant made before the policy existed.
func TestPolicyAskOutranksAStandingGrant(t *testing.T) {
	t.Parallel()
	grants := newFakeGrants()
	_ = grants.Grant(context.Background(), testIdentity, "skill_manage", "delete", "local")
	g, _, ctx := policyGateway(map[string]Policy{policyKey("skill_manage", "delete"): PolicyAsk})
	g.SetGrantStore(grants)
	v, err := g.Decide(ctx, gatedSpec(), gatedArgs(), testKey())
	if err != nil || v.Decision != Approve {
		t.Fatalf("Decide = %+v, %v; an always grant must not satisfy an ask", v, err)
	}
}

// The prompt under ask offers once and session, never always; an always answer relayed
// anyway resolves to once and writes no durable grant.
func TestPolicyAskOffersNoAlways(t *testing.T) {
	t.Parallel()
	grants := newFakeGrants()
	g, _, ctx := policyGateway(map[string]Policy{policyKey("skill_manage", "delete"): PolicyAsk})
	g.SetGrantStore(grants)
	v, err := g.Decide(ctx, gatedSpec(), gatedArgs(), testKey())
	if err != nil || v.ApprovalRequest == nil {
		t.Fatalf("Decide = %+v, %v; want a withheld approve", v, err)
	}
	var payload struct {
		Question string        `json:"question"`
		Options  []ScopeOption `json:"options"`
	}
	if err := json.Unmarshal([]byte(v.ApprovalRequest.Preview), &payload); err != nil {
		t.Fatalf("approval payload: %v", err)
	}
	if len(payload.Options) != 2 {
		t.Fatalf("options = %+v, want once and session only", payload.Options)
	}
	for _, o := range payload.Options {
		if strings.Contains(o.Value, ":always:") {
			t.Fatalf("an ask prompt offered %q", o.Value)
		}
	}
	always := scopeOptionValue(ScopeAlways, subjectFor(gatedSpec(), gatedArgs()))
	scope, err := g.ApproveChallenge(ctx, ApprovalAccept{
		ConversationID: testKey().ConversationID, Tool: gatedSpec().Name,
		ArgsFingerprint: gatewayArgsFingerprint(gatedArgs()), Question: payload.Question,
		Answer: always, IdentityID: testIdentity, OperatorID: "local",
	})
	if err != nil || scope != ScopeOnce {
		t.Fatalf("an always answer under ask = %q, %v; want once", scope, err)
	}
	if grants.granted != 0 {
		t.Fatalf("durable grants written = %d, want 0", grants.granted)
	}
}

// A failing policy read prompts: never a silent run of a tool that may be denied.
func TestPolicyLookupErrorFallsToThePrompt(t *testing.T) {
	t.Parallel()
	g, store, ctx := approvedGateway()
	g.SetPolicyStore(&fakePolicies{err: errors.New("connection reset")})
	v, err := g.Decide(ctx, readOnlySpec(), json.RawMessage(`{"path":"x"}`), testKey())
	if err != nil || v.Decision != Approve {
		t.Fatalf("Decide under a failing policy read = %+v, %v; want approve", v, err)
	}
	if n := len(store.reserves()); n != 0 {
		t.Fatalf("a prompted call reserved %d slots before the answer", n)
	}
}

// Headless runs keep their posture: an ask with no responder is a guided deny.
func TestPolicyAskUnderHeadlessDeniesWithGuidance(t *testing.T) {
	t.Parallel()
	g := New(config.ProfileSingleUserHardened, &fakeStore{})
	g.SetPolicyStore(newFakePolicies(map[string]Policy{policyKey("read_file", ""): PolicyAsk}))
	ctx := identityctx.WithIdentityID(context.Background(), testIdentity)
	v, err := g.Decide(ctx, readOnlySpec(), json.RawMessage(`{"path":"x"}`), testKey())
	if err != nil || v.Decision != Deny || !strings.Contains(v.Reason, "no interactive approver") {
		t.Fatalf("headless ask = %+v, %v; want the no-approver deny", v, err)
	}
}

// Dev and local_trusted are a no-op in Decide, so policies are inert there.
func TestPolicyIsInertUnderDevProfile(t *testing.T) {
	t.Parallel()
	for _, profile := range []config.RuntimeProfile{config.ProfileDev, config.ProfileLocalTrusted} {
		g := New(profile, &fakeStore{})
		g.SetPolicyStore(newFakePolicies(map[string]Policy{policyKey("shell_exec", ""): PolicyDeny}))
		ctx := identityctx.WithIdentityID(WithResponder(context.Background()), testIdentity)
		v, err := g.Decide(ctx, ordinaryWriteSpec(), json.RawMessage(`{"command":"ls"}`), testKey())
		if err != nil || v.Decision != Allow {
			t.Fatalf("%s: Decide = %+v, %v; want allow", profile, v, err)
		}
	}
}

// No store, no identity or no subject is no policy.
func TestPolicyForWithoutAStoreOrIdentityIsNone(t *testing.T) {
	t.Parallel()
	g := New(config.ProfileSingleUserHardened, &fakeStore{})
	if p := g.policyFor(context.Background(), testIdentity, grantSubject{Tool: "x"}); p != "" {
		t.Fatalf("no store = %q", p)
	}
	g.SetPolicyStore(newFakePolicies(map[string]Policy{policyKey("x", ""): PolicyDeny}))
	if p := g.policyFor(context.Background(), "", grantSubject{Tool: "x"}); p != "" {
		t.Fatalf("no identity = %q", p)
	}
	if p := g.policyFor(context.Background(), testIdentity, grantSubject{}); p != "" {
		t.Fatalf("no subject = %q", p)
	}
	var nilGateway *Gateway
	nilGateway.SetPolicyStore(newFakePolicies(nil))
}
