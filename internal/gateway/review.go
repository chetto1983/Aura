// review.go is the decide-only half of the gateway: what Decide would conclude about a call,
// read without acquiring an operation, reserving, recording a fact or issuing a challenge.
// Decide acts on that reading; Review turns it into the card a parked call shows.
package gateway

import (
	"context"
	"encoding/json"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/approvaltext"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/messagedrafts"
	"github.com/chetto1983/aura/internal/scoring"
)

// Review is the gateway's answer to "does this call need the operator?". When Needed, it
// carries the card: the question, its presentation and the scope options the operator
// chooses from, and the subject a session or always answer is keyed on.
type Review struct {
	Needed                     bool
	Tier                       scoring.RiskTier
	Question                   string
	Presentation               approvaltext.Presentation
	Options                    []ScopeOption
	SubjectTool, SubjectAction string
}

// needKind is what the decide-only reading concluded, one value per exit of Decide's order.
type needKind int

const (
	// needNone: nothing stops the call. A read-only call gets a decision fact, a write is reserved.
	needNone needKind = iota
	needDeniedByPolicy
	needPerson
	needNoApprover
	// needAllowed: an operator's accept or a standing grant already answered the question.
	needAllowed
	// needApproval: the operator must be asked.
	needApproval
)

type need struct {
	kind       needKind
	spec       tools.Spec // Mutating as Decide treats it
	tier       scoring.RiskTier
	subject    grantSubject
	policy     Policy
	operatorID string
	scope      ApprovalScope
}

// needFor reads policy, grants and the accept in Decide's order, and writes nothing.
//
// The hard deny comes first among the gated steps (WR-01): under server_production, or with
// no responder, a gated call is refused before any authorization or grant is read, so neither
// an accept nor an "always" grant lets a headless or production run through.
//
// An ungated write still reads the accept, only to name the operator on its reservation: a
// tool-sourced approval is for a call the tier does not gate. The accept never turns a refusal
// into an allow.
func (g *Gateway) needFor(ctx context.Context, spec tools.Spec, rawArgs json.RawMessage, key ReservationKey) need {
	if spec.Destructive {
		spec.Mutating = true
	}
	n := need{tier: classify(spec, rawArgs), subject: subjectFor(spec, rawArgs)}
	n.policy = g.policyFor(ctx, identityctx.IdentityID(ctx), n.subject)
	if n.policy == PolicyAsk {
		spec.Mutating = true
	}
	n.spec = spec
	switch {
	case n.policy == PolicyDeny:
		n.kind = needDeniedByPolicy
	case needsPerson(n.subject) && !personPresent(ctx):
		n.kind = needPerson
	case !spec.Mutating:
		n.kind = needNone
	case !gated(n.tier) && n.policy != PolicyAsk:
		n.kind = needNone
		if call, ok := approvedCall(ctx, spec, rawArgs, key); ok {
			n.operatorID, n.scope = call.OperatorID, ApprovalScope(call.Scope)
		}
	default:
		g.gatedNeed(ctx, rawArgs, key, &n)
	}
	return n
}

// gatedNeed is step 7 of Decide's order for a call the tier or an ask policy gates.
func (g *Gateway) gatedNeed(ctx context.Context, rawArgs json.RawMessage, key ReservationKey, n *need) {
	if g.profile == config.ProfileServerProduction || !responderPresent(ctx) {
		n.kind = needNoApprover
		return
	}
	if call, ok := approvedCall(ctx, n.spec, rawArgs, key); ok {
		n.kind, n.operatorID, n.scope = needAllowed, call.OperatorID, ApprovalScope(call.Scope)
		return
	}
	// Standing grants are keyed on the subject, never on the arguments, and reading one does
	// not spend it. An "always" grant does not answer an ask: the operator who set the policy
	// asked to be asked.
	if r, ok := g.approvals.SessionGrant(key.ConversationID, n.subject); ok && r.Approved {
		n.kind, n.operatorID, n.scope = needAllowed, r.OperatorID, ScopeSession
		return
	}
	if identityID := identityctx.IdentityID(ctx); n.policy != PolicyAsk && g.alwaysGranted(ctx, identityID, n.subject) {
		n.kind, n.operatorID, n.scope = needAllowed, identityID, ScopeAlways
		return
	}
	// A synchronous swarm worker inherits the turn's responder and has nobody on it.
	if !personPresent(ctx) {
		n.kind = needNoApprover
		return
	}
	n.kind = needApproval
}

// approvedCall is the operator's accept of exactly this call: the reservation key's
// conversation and call id, the tool's name and its arguments' fingerprint.
func approvedCall(ctx context.Context, spec tools.Spec, rawArgs json.RawMessage, key ReservationKey) (tools.ApprovedCall, bool) {
	call, ok := tools.ApprovedCallFor(ctx)
	if !ok || call.ConversationID != key.ConversationID || call.ToolCallID != key.ToolCallID || !call.Authorizes(spec.Name, rawArgs) {
		return tools.ApprovedCall{}, false
	}
	return call, true
}

// Review reports whether the call needs the operator and, if so, the card to park it with.
// It reserves nothing and records nothing; the call's dispatch decides again after the
// accept. A send the message review owns is never the gateway's card. The describer is asked
// only for a call that needs the operator; its error is not an approval, and the call goes to
// its normal dispatch.
func (g *Gateway) Review(ctx context.Context, spec tools.Spec, raw json.RawMessage, key ReservationKey, describer tools.ApprovalDescriber) (Review, error) {
	if g == nil || !g.profile.Strict() {
		return Review{}, nil
	}
	if _, outbound := messagedrafts.TrustedOutboundTarget(spec); outbound {
		return Review{}, nil
	}
	n := g.needFor(ctx, spec, raw, key)
	if n.kind != needApproval {
		return Review{Tier: n.tier}, nil
	}
	target, err := describeTarget(ctx, describer, raw)
	if err != nil {
		return Review{Tier: n.tier}, err
	}
	// The gateway card shows the argument keys, never their values, so secrets and message
	// bodies stay off the channel. What runs is pinned by the stored fingerprint, not displayed.
	card := approvaltext.Presentation{Key: approvaltext.GatewayKey, Params: map[string]string{
		"tool": n.spec.Name, "risk": string(n.tier), "args": argKeySummary(raw),
	}}
	if target != "" {
		card.Params["target"] = target
	}
	return Review{
		Needed:        true,
		Tier:          n.tier,
		Question:      gatewayApprovalQuestion(n.spec, n.tier, raw),
		Presentation:  card,
		Options:       scopeOptions(n.subject, n.policy),
		SubjectTool:   n.subject.Tool,
		SubjectAction: n.subject.Action,
	}, nil
}
