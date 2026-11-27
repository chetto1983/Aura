package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
)

// countingDescriber is a tools.ApprovalDescriber that counts how often it is asked.
type countingDescriber struct {
	target string
	err    error
	calls  int
}

func (d *countingDescriber) DescribeForApproval(context.Context, json.RawMessage) (string, error) {
	d.calls++
	return d.target, d.err
}

var _ tools.ApprovalDescriber = (*countingDescriber)(nil)

// The describer runs only for a call about to be shown to the operator: the profile, a deny
// policy, the no-responder deny, the standing grants and the operator's one-shot accept all
// decide first (spec 2026-10-12). A call one of them decided is never described.
func TestDescriberRunsOnlyRightBeforeThePrompt(t *testing.T) {
	t.Parallel()
	args := gatedArgs()
	approved := ResolvedApproval{Approved: true, OperatorID: "local"}
	cases := []struct {
		name  string
		build func() (*Gateway, context.Context)
		spec  tools.Spec
		want  Decision
	}{
		{"dev profile", func() (*Gateway, context.Context) {
			return New(config.ProfileDev, &fakeStore{}), WithResponder(context.Background())
		}, gatedSpec(), Allow},
		{"deny policy", func() (*Gateway, context.Context) {
			g, _, ctx := policyGateway(map[string]Policy{policyKey("skill_manage", "delete"): PolicyDeny})
			return g, ctx
		}, gatedSpec(), Deny},
		{"no responder", func() (*Gateway, context.Context) {
			return New(config.ProfileSingleUserHardened, &fakeStore{}), context.Background()
		}, gatedSpec(), Deny},
		{"session grant", func() (*Gateway, context.Context) {
			g, _, ctx := approvedGateway()
			g.approvals.GrantSession(testKey().ConversationID, subjectFor(gatedSpec(), args), approved)
			return g, ctx
		}, gatedSpec(), Allow},
		{"always grant", func() (*Gateway, context.Context) {
			g, _, ctx := approvedGateway()
			grants := newFakeGrants()
			grants.rows["skill_manage\x00delete"] = "local"
			g.SetGrantStore(grants)
			return g, ctx
		}, gatedSpec(), Allow},
		{"operator's accept", func() (*Gateway, context.Context) {
			g, _, ctx := approvedGateway()
			g.RecordResolvedApproval(testKey().ConversationID, gatedSpec().Name, tools.CallFingerprint(args), approved)
			return g, ctx
		}, gatedSpec(), Allow},
		{"ungated call without a policy", func() (*Gateway, context.Context) {
			g, _, ctx := approvedGateway()
			return g, ctx
		}, ordinaryWriteSpec(), Allow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g, ctx := tc.build()
			d := &countingDescriber{target: "Fattura fornitore · Fatto"}
			v, err := g.Decide(WithApprovalDescriber(ctx, d), tc.spec, args, testKey())
			if err != nil || v.Decision != tc.want {
				t.Fatalf("Decide = %+v, %v; want %s", v, err, tc.want)
			}
			if d.calls != 0 {
				t.Fatalf("described %d time(s) a call that never reached the prompt", d.calls)
			}
		})
	}
	g, _, ctx := approvedGateway()
	d := &countingDescriber{target: "Fattura fornitore · Fatto"}
	if v, err := g.Decide(WithApprovalDescriber(ctx, d), gatedSpec(), args, testKey()); err != nil || v.Decision != Approve || d.calls != 1 {
		t.Fatalf("Decide = %+v, %v with %d description(s); want the prompt, described once", v, err, d.calls)
	}
}

// The question a described call relays is the one any call gets. The target stays on the
// challenge, through approvalLine, beside the withheld call's tool_call_id; it never enters
// the request the model copies (spec 2026-10-12).
func TestDescribedTargetStaysOnTheChallenge(t *testing.T) {
	t.Parallel()
	const question = "Approve skill_manage (risk=destructive)?\nThis mutating action is WITHHELD until you accept.\nargs: action, name"
	fp := tools.CallFingerprint(gatedArgs())
	tests := []struct {
		name, label, want string
	}{
		{"described", "Fattura dell’Enel “urgente”\n<b>· Fatto", "Fattura dell’Enel “urgente” b· Fatto"},
		{"not described", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g, _, ctx := approvedGateway()
			v, err := g.Decide(WithApprovalDescriber(ctx, &countingDescriber{target: tt.label}), gatedSpec(), gatedArgs(), testKey())
			if err != nil || v.ApprovalRequest == nil {
				t.Fatalf("Decide = %+v, %v; want an approval request", v, err)
			}
			if got := previewQuestion(t, *v.ApprovalRequest); got != question {
				t.Fatalf("question = %q, want it unchanged: %q", got, question)
			}
			if tt.label != "" && strings.Contains(v.ApprovalRequest.Preview, "Enel") {
				t.Fatalf("the target reached the request the model relays: %q", v.ApprovalRequest.Preview)
			}
			target, toolCallID, ok := g.ApprovalChallengeDetails(testKey().ConversationID, gatedSpec().Name, fp)
			if !ok || target != tt.want || toolCallID != testKey().ToolCallID {
				t.Fatalf("challenge = %q, %q, %v; want %q for call %q", target, toolCallID, ok, tt.want, testKey().ToolCallID)
			}
			if _, _, ok := g.ApprovalChallengeDetails("another-conversation", gatedSpec().Name, fp); ok {
				t.Fatal("a lookup under another conversation found this challenge")
			}
		})
	}
}

// A describer that refuses stops the call as a failure: before the prompt, before anything is
// reserved, and without a deny fact, because it is not the gateway's refusal (spec
// 2026-10-12). The call is a Normal-tier write an ask policy sends to the prompt, the shape
// whose reservation would otherwise follow at once.
func TestDescriberErrorRefusesBeforeAnythingIsReserved(t *testing.T) {
	t.Parallel()
	refusal := errors.New("card c1 is not on the board you listed: run board list or board search first")
	g, store, ctx := policyGateway(map[string]Policy{policyKey("shell_exec", ""): PolicyAsk})
	args := json.RawMessage(`{"command":"ls"}`)
	if _, err := g.Decide(WithApprovalDescriber(ctx, &countingDescriber{err: refusal}), ordinaryWriteSpec(), args, testKey()); !errors.Is(err, refusal) {
		t.Fatalf("err = %v, want the describer's refusal", err)
	}
	if n, m := len(store.reserves()), len(store.calls()); n != 0 || m != 0 {
		t.Fatalf("reserved %d and recorded %d row(s) for a call refused before the prompt", n, m)
	}
	if _, _, ok := g.ApprovalChallengeDetails(testKey().ConversationID, "shell_exec", tools.CallFingerprint(args)); ok {
		t.Fatal("a refused call left a challenge behind")
	}
	v, err := g.Decide(WithApprovalDescriber(ctx, &countingDescriber{target: "ls"}), ordinaryWriteSpec(), args, testKey())
	if err != nil || v.Decision != Approve || len(store.reserves()) != 0 {
		t.Fatalf("Decide = %+v, %v with %d reservation(s); want the prompt and nothing reserved", v, err, len(store.reserves()))
	}
}

// The runner looks challenges up on every saved gateway pause, so the lookup holds on a
// gateway with nothing behind it.
func TestApprovalChallengeDetailsIsNilSafe(t *testing.T) {
	t.Parallel()
	var none *Gateway
	for _, g := range []*Gateway{none, {}} {
		if _, _, ok := g.ApprovalChallengeDetails("c", "t", "f"); ok {
			t.Fatalf("%+v found a challenge", g)
		}
	}
}
