package channels

import "context"

// Deliverer is an OPTIONAL channel capability: a started Channel MAY also
// implement it. The Registry runtime-asserts ch.(Deliverer) and skips a channel
// that does not — zero registry change for a future channel.
//
// The tri-state return is load-bearing — the dispatch precedence (Phase 20 R4,
// plan 20-03) and the Registry fan-out (R2) both depend on it:
//
//	(false, nil) = not my user      → try the next channel
//	(true,  nil) = delivered        → stop
//	(false, err) = owns-but-failed  → stop, do NOT try siblings
//
// (false, err) must NOT be read as "try the next channel": that would let one
// identity's message leak to a second channel on a transient error (double-
// delivery). The contract travels with the type so every consumer honors it.
type Deliverer interface {
	Deliver(ctx context.Context, identityID, text string) (delivered bool, err error)
}

// ApprovalDeliverer is an OPTIONAL channel capability distinct from Deliverer: a channel MAY
// implement it to render an ACTIONABLE approval prompt (inline accept/reject bound to the pause
// token) rather than plain text. The scheduler approval-reminder sweep uses it to re-surface a
// pending scheduled_task_approval as a real HITL prompt on the origin channel (Telegram Sì/No);
// pressing a button resolves the real ask_user pause (Amendment #92 revised / fix-plan 1.7).
//
// It honors the SAME tri-state contract as Deliverer:
//
//	(false, nil) = not my user      → try the next channel
//	(true,  nil) = delivered        → stop
//	(false, err) = owns-but-failed  → stop, do NOT try siblings
//
// A pull-based surface (the WebUI cockpit) is NOT a channel and does not implement this — it
// reads the pause from /api/approvals — so DeliverApproval returning (false, nil) for a
// WebUI-origin identity is expected and correct (the caller does not treat it as an error).
type ApprovalDeliverer interface {
	DeliverApproval(ctx context.Context, identityID, token, taskID, kind string) (delivered bool, err error)
}
