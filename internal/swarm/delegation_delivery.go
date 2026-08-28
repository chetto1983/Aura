// delegation_delivery.go is the single delivery concern a claimed delegation's
// terminal success runs through (plan 51-10, SWARM-03/09): the SC#1 write --
// appending the consolidated report to its origin conversation as an
// out-of-band assistant turn, so the result is observable in
// aura.conversation_turns whether or not an operator turn is running -- the
// unchanged present-operator steer push (D-04), and the absent-operator
// channel nudge for a report the operator never drained (D-02).
//
// Split out of delegation_queue.go (already full: enqueue + claim + retry)
// rather than grown inline -- CLAUDE.md's NO GOD CLASS rule and this
// package's own brief.go/report.go/swarm_depth.go split precedent. Both
// ConversationRecorder and ChannelDeliverer are declared HERE, in the
// consuming package, and adapted onto the conversations store /
// the channels registry at cmd/aura -- this package gains NO import edge into
// either of those two packages (D-02's closed shape: no new messaging tool,
// no messaging schema, no new dependency).
package swarm

import (
	"context"
	"time"
)

// ConversationRecorder appends a finished delegation's consolidated report to
// the conversation it was spawned from, out of band -- the SC#1 write itself.
// Mirrors internal/cron/dispatch.go's OWN ConversationRecorder seam one layer
// further out (a worker instead of a scheduled task): same method, same
// consumer-declared-interface idiom, same reason (D-00, LibreChat: the
// conversation IS the channel). *conversations.Store satisfies it via
// cmd/aura/serve_delegation.go's adapter (Seq 0, matching serve_dispatch.go's
// own out-of-band append).
type ConversationRecorder interface {
	AppendAssistantTurn(ctx context.Context, conversationID, text string) error
}

// ChannelDeliverer pushes text to whatever channel owns identityID -- the
// SHIPPED tri-state contract internal/cron/deliver.go's own ChannelDeliverer
// already establishes, reused verbatim rather than redefined:
// (true,nil)=delivered; (false,nil)=nobody owns the identity;
// (false,err)=owns-but-failed. Declared HERE so this package gains no import
// edge into the channels package; *channels.Registry satisfies it via
// DeliverToIdentity (20-01) by construction.
type ChannelDeliverer interface {
	DeliverToIdentity(ctx context.Context, identityID, text string) (delivered bool, err error)
}

// UndrainedResult is one aura.steer_queue delegation_result row past the
// nudge grace window that the operator never drained (drained_at IS NULL).
type UndrainedResult struct {
	ID         string
	IdentityID string
	Body       string
}

// SteerNudgeStore is the absent-operator leg's read/write seam over
// aura.steer_queue's nudge columns (drained_at/nudged_at, plan 51-02).
// Declared here and adapted at cmd/aura -- internal/steer independently
// declares its own row type for the identical query (internal/steer must
// never import internal/swarm, which already imports internal/steer for
// steer.SourceWorker below, so a shared type would cycle); the cmd/aura
// adapter is the one translation layer that shape difference costs.
type SteerNudgeStore interface {
	ListUnnudgedDelegationResults(ctx context.Context, cutoff time.Time, limit int) ([]UndrainedResult, error)
	MarkSteerRowNudged(ctx context.Context, id, identityID string) (bool, error)
}

// PendingNotificationStore is the retry-outbox seam the absent-operator leg's
// owns-but-failed branch needs (migration 0105's steer_queue_id leg):
// declared here, adapted at cmd/aura onto *cron.Store -- the SAME store
// internal/cron/deliver.go's own owns-but-failed leg already writes through,
// never a second outbox (D-02).
type PendingNotificationStore interface {
	InsertPendingNotification(ctx context.Context, steerQueueID, identityID, body, lastErr string) error
}

// DelegationDelivery is the single delivery concern a claimed delegation's
// terminal success (Deliver) and the periodic absent-operator sweep
// (NudgeUndrained) both run through. Every collaborator is optional in the Go
// nil-safe sense EXCEPT Recorder, which is load-bearing for SC#1: without it
// Deliver refuses rather than silently skipping the write a result reaching
// nobody would otherwise vanish behind (see Deliver's own doc).
type DelegationDelivery struct {
	Recorder ConversationRecorder
	Steer    SteerPublisher
	Channel  ChannelDeliverer
	Nudge    SteerNudgeStore
	Pending  PendingNotificationStore
	// NudgeAfter is AURA_SWARM_DELEGATION_NUDGE_SEC. <=0 disables the channel
	// leg entirely (the shipped AURA_ASKUSER_PAUSE_TTL_SEC <=0-disables
	// precedent): NudgeUndrained becomes a no-op, leaving record + steer only.
	NudgeAfter time.Duration
}

// NewDelegationDelivery builds a DelegationDelivery from its collaborators.
func NewDelegationDelivery(
	recorder ConversationRecorder,
	steerPub SteerPublisher,
	channel ChannelDeliverer,
	nudge SteerNudgeStore,
	pending PendingNotificationStore,
	nudgeAfter time.Duration,
) *DelegationDelivery {
	return &DelegationDelivery{
		Recorder:   recorder,
		Steer:      steerPub,
		Channel:    channel,
		Nudge:      nudge,
		Pending:    pending,
		NudgeAfter: nudgeAfter,
	}
}

// Deliver is not yet implemented (RED, plan 51-10 Task 1): a stub that
// records nothing and pushes nothing, so every test asserting the SC#1
// write's real behavior fails for the right reason ahead of the GREEN commit.
func (d *DelegationDelivery) Deliver(ctx context.Context, payload DelegationPayload, text string) (recorded bool, err error) {
	return false, nil
}

// NudgeUndrained is not yet implemented (RED, plan 51-10 Task 2): a stub that
// nudges nothing, so every tri-state/idempotency test fails for the right
// reason ahead of the GREEN commit.
func (d *DelegationDelivery) NudgeUndrained(ctx context.Context, now time.Time, limit int) (int, error) {
	return 0, nil
}
