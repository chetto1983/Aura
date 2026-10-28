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

// ConversationDeliverer is the conversation-scoped counterpart to Deliverer.
// A channel returns true only when both the identity and conversation belong to
// that channel. Owning the identity alone is insufficient: asynchronous outcomes
// must stay on the surface they originated from. The tri-state contract is the
// same as Deliverer.
type ConversationDeliverer interface {
	DeliverConversation(ctx context.Context, identityID, conversationID, text string) (delivered bool, err error)
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

// TelegramAccountDeliverer is the Telegram channel's capability to push to one account an identity
// links, by its Telegram user id, for a scheduled task that names its recipients
// (docs/superpowers/specs/2026-10-28-telegram-push-recipients-design.md). The tri-state contract
// is Deliverer's, with one difference: an account the identity no longer links is an error, never
// "not mine", because the push skips it and must not reach another account. An error whose
// Permanent() reports true (errors.As) cannot succeed on a retry: the account is unlinked, the bot
// was blocked, the chat is gone.
type TelegramAccountDeliverer interface {
	DeliverToTelegramAccount(ctx context.Context, identityID string, telegramUserID int64, text string) (delivered bool, err error)
	DeliverApprovalToTelegramAccount(ctx context.Context, identityID string, telegramUserID int64, token, taskID, kind string) (delivered bool, err error)
}
