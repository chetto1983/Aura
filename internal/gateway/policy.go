// policy.go holds the narrowing half of the approval model (prd.md §5, "A tool policy per
// identity", 2026-10-09): the per-identity `ask` and `deny` rows an operator sets with
// `aura gateway policy` or the cockpit. The three approval scopes in scope.go only widen;
// a policy is how an operator asks to be asked more, or forbids one tool outright.
//
// Precedence, applied in Decide: deny, then ask, then grant, then tier. A policy binds at
// the next decision and never interrupts a running call; under the dev and local_trusted
// profiles Decide is a no-op, so policies are inert there too.
package gateway

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/chetto1983/aura/internal/obs"
)

// Policy is an identity's standing narrowing of one subject. The zero value means no
// policy: the subject falls through to grants and its tier.
type Policy string

const (
	// PolicyAsk routes the subject to approval whatever its tier, through the reservation
	// funnel; its prompt offers no "always", and an "always" grant does not satisfy it.
	PolicyAsk Policy = "ask"
	// PolicyDeny refuses the call before execution with a reason the model reads.
	PolicyDeny Policy = "deny"
)

// policyStore is the durable seam. internal/approvalpolicies.Store satisfies it through a
// one-line adapter at the composition root, so the gateway imports no store package — the
// same line grantStore draws.
type policyStore interface {
	Get(ctx context.Context, identityID, tool, action string) (Policy, bool, error)
}

// SetPolicyStore wires the durable policy store. Nil leaves every subject with no policy,
// which is the pre-policy behaviour.
func (g *Gateway) SetPolicyStore(s policyStore) {
	if g != nil {
		g.policies = s
	}
}

var policyDecisions = func() metric.Int64Counter {
	descriptor := obs.MustDescriptor(obs.GatewayPolicyDecisionsID)
	counter, err := otel.Meter("github.com/chetto1983/aura/internal/gateway").Int64Counter(
		descriptor.Name, metric.WithDescription(descriptor.Description), metric.WithUnit(descriptor.Unit))
	if err != nil {
		panic(err)
	}
	return counter
}()

// policyFor reads the identity's policy for subject. A missing store or an empty identity
// is no policy. A store error is PolicyAsk with one warning: a database blip then costs the
// operator one prompt they can decline, never a silent run of a tool they may have denied,
// and never a refusal of every tool while the database is unreachable.
func (g *Gateway) policyFor(ctx context.Context, identityID string, subject grantSubject) Policy {
	if g.policies == nil || identityID == "" || subject.Tool == "" {
		return ""
	}
	policy, ok, err := g.policies.Get(ctx, identityID, subject.Tool, subject.Action)
	if err != nil {
		slog.Warn("gateway: tool policy lookup failed; asking the operator",
			"identity_id", identityID, "tool", subject.Tool, "action", subject.Action, "err", err)
		policy, ok = PolicyAsk, true
	}
	if !ok {
		return ""
	}
	outcome := "ask"
	if policy == PolicyDeny {
		outcome = "denied"
	}
	policyDecisions.Add(ctx, 1, metric.WithAttributes(attribute.String(
		string(obs.AttributeOutcome), obs.NormalizeAttribute(obs.AttributeOutcome, outcome))))
	return policy
}

// policyDenyReason is what the model reads when a policy refused its call: the subject, and
// the instruction not to retry, because a retry of a denied tool is a wasted round.
func policyDenyReason(subject grantSubject) string {
	return "tool \"" + subject.String() + "\" is disabled for this identity by policy: do not retry it, tell the operator"
}
