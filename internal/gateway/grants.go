// grants.go holds the durable half of amendment #127: the narrow seam the ScopeAlways
// grants are read and written through, and the accept payload the host-side resume hook
// hands the gateway.
//
// ScopeOnce and ScopeSession live entirely in GatewayApprovals' memory — losing them to a
// restart costs the operator one extra approval, which is the right failure. ScopeAlways is
// the one an operator expects to survive a restart, so it is the only one that reaches
// Postgres, and it is identity-scoped: a grant is a statement by a principal about their own
// agent, never a property of the process.
package gateway

import (
	"context"
	"log/slog"
)

// grantStore is the durable ScopeAlways seam. *approvalgrants.Store satisfies it. It is
// deliberately narrower than the store: list and revoke are operator verbs the CLI calls
// directly, and the gateway has no business holding them.
type grantStore interface {
	Grant(ctx context.Context, identityID, tool, action, grantedBy string) error
	Has(ctx context.Context, identityID, tool, action string) (bool, error)
}

// ApprovalAccept is one operator accept as the host-side resume hook observed it: the
// authenticated coordinates of the pause, the operator-visible question (the CR-01
// informed-consent binding), the label they chose, and who they are. It is a struct rather
// than seven positional strings because six of the seven are strings and a transposed pair
// would silently authorize the wrong thing.
type ApprovalAccept struct {
	// ConversationID is the AUTHENTICATED, server-stored conversation of the pause — never
	// the model-relayed resume_context id (WR-02).
	ConversationID string
	// Tool and ArgsFingerprint address the challenge routeApprove issued.
	Tool            string
	ArgsFingerprint string
	// Question is what the operator was actually shown; it must equal the gateway-generated
	// one or nothing is recorded (CR-01).
	Question string
	// Answer is the label the operator chose. It resolves to a scope against the challenge's
	// own label table; anything unrecognised resolves to ScopeOnce.
	Answer string
	// IdentityID is the authenticated principal a ScopeAlways grant belongs to. An empty
	// identity CANNOT hold a durable grant: the accept still stands, but it degrades to
	// ScopeSession rather than being attributed to a principal nobody named.
	IdentityID string
	// OperatorID is the attribution folded into the reservation start Meta.
	OperatorID string
}

// SetGrantStore wires the durable ScopeAlways store. Nil leaves the gateway with the two
// in-memory scopes only, which is the correct dev/standalone posture: without a store an
// "always" accept degrades to a session grant rather than silently vanishing.
func (g *Gateway) SetGrantStore(s grantStore) {
	if g != nil {
		g.grants = s
	}
}

// alwaysGranted reports whether identityID holds a durable grant for subject. A missing
// store, an empty identity, or a store error is NOT a grant: the call falls through to the
// approval challenge, which is the fail-closed direction. The error is logged rather than
// returned because a policy READ failing must not fail the turn — it must fail to the prompt.
func (g *Gateway) alwaysGranted(ctx context.Context, identityID string, subject grantSubject) bool {
	if g == nil || g.grants == nil || identityID == "" || subject.Tool == "" {
		return false
	}
	ok, err := g.grants.Has(ctx, identityID, subject.Tool, subject.Action)
	if err != nil {
		slog.Warn("gateway: always-grant lookup failed; falling through to approval",
			"identity_id", identityID, "tool", subject.Tool, "action", subject.Action, "err", err)
		return false
	}
	return ok
}

// recordAlwaysGrant persists the operator's ScopeAlways choice and reports the scope that
// actually took effect. It degrades to ScopeSession — never silently to nothing — when there
// is no store, no identity, or the write fails: the operator asked for the widest scope, and
// answering that with the next-widest one they can actually be given is honest, where
// reporting "always" over a failed INSERT is not.
func (g *Gateway) recordAlwaysGrant(ctx context.Context, a ApprovalAccept, subject grantSubject) ApprovalScope {
	switch {
	case g == nil || g.grants == nil:
		slog.Warn("gateway: no durable grant store; 'always' recorded for this conversation only",
			"tool", subject.Tool, "action", subject.Action)
	case a.IdentityID == "":
		slog.Warn("gateway: accept carries no identity; 'always' recorded for this conversation only",
			"tool", subject.Tool, "action", subject.Action)
	default:
		if err := g.grants.Grant(ctx, a.IdentityID, subject.Tool, subject.Action, a.OperatorID); err != nil {
			slog.Warn("gateway: durable grant write failed; 'always' recorded for this conversation only",
				"identity_id", a.IdentityID, "tool", subject.Tool, "action", subject.Action, "err", err)
			break
		}
		slog.Info("gateway: durable approval grant recorded",
			"identity_id", a.IdentityID, "tool", subject.Tool, "action", subject.Action,
			"granted_by", a.OperatorID)
		return ScopeAlways
	}
	g.approvals.GrantSession(a.ConversationID, subject, ResolvedApproval{Approved: true, OperatorID: a.OperatorID})
	return ScopeSession
}
