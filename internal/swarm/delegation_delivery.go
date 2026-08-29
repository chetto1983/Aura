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
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/steer"
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

// ChannelDeliverer pushes text only to a channel that owns the exact
// (identity, conversation) pair. The tri-state contract remains:
// (true,nil)=delivered; (false,nil)=no channel owns that conversation;
// (false,err)=owns-but-failed. Declared here so swarm gains no import edge
// into channels; *channels.Registry satisfies it by construction.
type ChannelDeliverer interface {
	DeliverToConversation(ctx context.Context, identityID, conversationID, text string) (delivered bool, err error)
}

// UndrainedResult is one aura.steer_queue delegation_result row past the
// nudge grace window that the operator never drained (drained_at IS NULL).
type UndrainedResult struct {
	ID             string
	IdentityID     string
	ConversationID string
	Body           string
	// FanoutKey groups the N terminal rows produced by one swarm_spawn call. It is
	// mandatory for delegation_result rows after migration 0109.
	FanoutKey string
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
	// MarkFanoutNudged claims every unclaimed row of one identity/fan-out pair in one
	// statement; see delegation_fanout.go for the claim-before-push invariant.
	MarkFanoutNudged(ctx context.Context, identityID, fanoutKey string) ([]UndrainedResult, error)
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
	// Archiver persists a terminal report's full markdown as an owned asset
	// (51-11, UI-SPEC §2). nil-safe: DeliverReport degrades to a card with no
	// artifact pointer rather than failing (delegation_artifact.go).
	Archiver ReportArchiver
	// Counter is the fan-out eligibility check (51-11 Task 3, delegation_fanout.go's
	// nudgeFanout). nil-safe: a nil Counter degrades every fan-out to "eligible" --
	// today's behaviour before this task, never a delivery block forever.
	Counter FanoutJobCounter
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
	archiver ReportArchiver,
	counter FanoutJobCounter,
	nudgeAfter time.Duration,
) *DelegationDelivery {
	return &DelegationDelivery{
		Recorder:   recorder,
		Steer:      steerPub,
		Channel:    channel,
		Nudge:      nudge,
		Pending:    pending,
		Archiver:   archiver,
		Counter:    counter,
		NudgeAfter: nudgeAfter,
	}
}

// Deliver runs the SC#1 write, THEN the present-operator steer push -- the
// order is load-bearing (a push is not a record; the record is the durable
// copy and the push is the courtesy, never the reverse). recorded reports
// whether the conversation record succeeded, so the caller
// (delegation_queue.go's deliverSuccess) can gate the job's succeeded
// transition on it: a report that reached neither the conversation nor (once
// nudged) a channel has not been delivered and must be retried by the
// shipped backoff, never marked succeeded by omission.
//
// An empty report records nothing and pushes nothing (recorded=true: there is
// nothing to retry, so this is not a delivery failure).
//
// A push (steer.Push) failure is a hard infrastructure error, returned
// unchanged -- the SAME contract deliverSuccess carried before this plan
// (51-01). A RECORD failure is NOT a hard Go error: it is a WARN (the work
// already happened; only the durable copy failed to write) reflected solely
// through recorded=false, mirroring internal/cron/dispatch.go's own
// recordToOrigin ("a failed write is a WARN and never a run failure").
//
// The RECORDED copy is attributed (T-51-38, threat model "mitigate"): it is
// wrapped naming the worker and the goal, never written as Aura's own
// unqualified words, so a LATER turn re-reading aura.conversation_turns
// cannot mistake a worker's output for the assistant's own conclusion. The
// PUSHED copy (text, unwrapped) is unchanged from 51-01 -- it gets its own
// attribution downstream, at drain time, from markSteer/
// wrapUntrustedToolOutput (internal/agent/llm_agent_steer.go), which already
// treats steer.SourceWorker as untrusted content. These are two separate
// envelopes for two separate readers (durable history vs. a live turn), not
// one shared wrapping.
func (d *DelegationDelivery) Deliver(ctx context.Context, payload DelegationPayload, text string) (recorded bool, err error) {
	if d == nil || d.Recorder == nil {
		return false, fmt.Errorf("swarm: delegation delivery has no conversation recorder configured")
	}
	if strings.TrimSpace(text) == "" {
		return true, nil
	}
	if rerr := d.Recorder.AppendAssistantTurn(ctx, payload.ConversationID, attributedWorkerReport(payload.Goal, text)); rerr != nil {
		slog.Warn("swarm.delegation.record_failed",
			"conversation", payload.ConversationID, "err", rerr)
		recorded = false
	} else {
		recorded = true
	}
	if d.Steer != nil {
		if perr := d.Steer.Push(payload.ConversationID, steer.SourceWorker, text); perr != nil {
			return recorded, fmt.Errorf("delegation report push: %w", perr)
		}
	}
	return recorded, nil
}

// attributedWorkerReport is the SC#1 conversation record's own attribution
// (T-51-38): names the delegated worker's goal so a later turn re-reading
// history reads this as reported-by-a-worker content, never as Aura's own
// unqualified words. Distinct from the untrusted-tool-output envelope the
// steer rail applies at drain time (internal/agent/llm_agent_steer.go) --
// that envelope is minted per-drain for a live model turn; this one is
// written once, durably, into aura.conversation_turns itself.
func attributedWorkerReport(goal, text string) string {
	return fmt.Sprintf("[Delegated worker report -- goal: %q]\n%s", goal, text)
}

// DeliverReport is Deliver's counterpart for a TERMINAL worker outcome
// (51-11, SWARM-12 legs 1+2): it archives the full
// report markdown FIRST (best-effort, delegation_artifact.go), THEN records
// the bounded card (never the raw report JSON Amendment #172 measured
// landing in aura.conversation_turns), THEN pushes a bounded ChildReport JSON
// projection on the steer rail. The full uncapped report remains in the
// archive; bounding the courtesy copy prevents a completed worker from being
// retried solely because JSON encoding exceeds steer's message cap (Amendment
// #178). The ordering remains the SAME record-before-push ordering Deliver's
// own doc comment states, with the archive step ahead of both because the
// card's own artifact-pointer line needs the archived filename to exist first.
//
// recorded/err carry the SAME contract as Deliver: recorded=false (never a
// Go error) on a conversation-append failure, so the caller
// (delegation_queue.go's deliverSuccess/recordFailure) still gates the row's
// terminal transition on it, never marking a lost report delivered by
// omission. A push (steer.Push) failure remains a hard infrastructure error.
//
// Push here calls Steer.PushDelegationResult carrying payload.FanoutKey. Deliver
// (a worker's question) keeps calling plain Steer.Push because it is not a terminal
// delegation_result and does not enter the absent-operator fan-out sweep.
func (d *DelegationDelivery) DeliverReport(ctx context.Context, payload DelegationPayload, report ChildReport, elapsed time.Duration) (recorded bool, err error) {
	if d == nil || d.Recorder == nil {
		return false, fmt.Errorf("swarm: delegation delivery has no conversation recorder configured")
	}
	if strings.TrimSpace(payload.FanoutKey) == "" {
		return false, fmt.Errorf("swarm: delegation report has no fan-out key")
	}
	identityID := identityctx.IdentityID(ctx)
	artifactName := archiveReport(ctx, d.Archiver, identityID, payload.ConversationID, report.ChildID,
		DelegationReportMarkdown(report, elapsed))

	card := DelegationRecordCard(report, elapsed, artifactName)
	if rerr := d.Recorder.AppendAssistantTurn(ctx, payload.ConversationID, card); rerr != nil {
		slog.Warn("swarm.delegation.record_failed",
			"conversation", payload.ConversationID, "err", rerr)
		recorded = false
	} else {
		recorded = true
	}

	if d.Steer != nil {
		text, merr := marshalReports([]ChildReport{boundedDeliveryReport(report)})
		if merr != nil {
			return recorded, fmt.Errorf("delegation report marshal: %w", merr)
		}
		if perr := d.Steer.PushDelegationResult(payload.ConversationID, steer.SourceWorker, text, payload.FanoutKey); perr != nil {
			return recorded, fmt.Errorf("delegation report push: %w", perr)
		}
	}
	return recorded, nil
}

// NudgeUndrained is the absent-operator leg's periodic sweep (SWARM-03/09).
// The unit of delivery is the FAN-OUT, not the row (51-11 Task 3, CONTEXT
// D-15, the operator's own words: "uno per fan-out") -- for every
// aura.steer_queue delegation_result row the operator has not drained
// (drained_at IS NULL) that is older than NudgeAfter, the sweep groups the
// candidates by fan-out key (groupByFanout, delegation_fanout.go) and pushes
// to the owning identity's channel EXACTLY ONCE per fan-out. A drained row (the
// operator was present, the steer rail already delivered it mid-turn) is
// never nudged -- the point is telling an ABSENT operator once, never
// repeating what a present one already received.
//
// NudgeAfter<=0 disables this leg entirely: record + steer keep working via
// Deliver, only the channel push is off.
//
// Claim BEFORE push, never the reverse -- the same "the drain IS the claim"
// idiom internal/db/queries/steer_queue.sql's own DrainSteerRows already
// uses, generalized from a FOR-UPDATE transaction to a conditional UPDATE:
// MarkFanoutNudged's `WHERE nudged_at IS NULL` is what
// makes two CONCURRENT sweep passes over the SAME fan-out
// push at most once (SWARM-09 edge). A bare SELECT for the candidate list,
// with the mark-as-claimed done only AFTER a successful push, would let two
// passes both observe the row(s) as unclaimed and both call
// DeliverToConversation before either commits -- a real double-push. Claiming
// first closes that window: the loser sees an empty MarkFanoutNudged result.
//
// The tri-state branch mirrors internal/cron/deliver.go's deliverToOrigin
// verbatim, because a delegation result has no NotifyRoute and no per-task
// route chain behind it (unlike a scheduled task, PRD Amendment #154):
// delivered -> stop (already claimed). Nobody owns the identity -> ALSO stop
// -- the conversation record Deliver/DeliverReport already wrote IS the
// delivery, and there is no per-task route to fall back to. Owns-but-failed
// -> insert ONE pending_notifications retry row per fan-out (migration
// 0105's steer_queue_id leg, the shipped outbox, referencing the fan-out's
// FIRST claimed row -- never N rows and never the raw report); the rows stay
// claimed (nudged_at is already set), so the retry from here on belongs
// entirely to pending_notifications' own sweep/backoff, never re-attempted
// by a later NudgeUndrained pass.
//
// What this does NOT prove (state plainly, per Amendment #154): the fan-out's
// choice between two candidate channels and its owns-but-failed leg have
// never been exercised live -- Telegram is the only shipped Deliverer -- and
// the outbox's retry/backoff/dead-letter behaviour is read from the schema,
// not measured. Extended by 51-11 Task 3's own named cost: a fan-out holding
// one worker parked in awaiting_input keeps the phone silent about its
// finished siblings until that question is answered or the row's TTL expires
// it (CountUnfinishedDelegationJobs counts awaiting_input as unfinished on
// purpose), while the cockpit card for each sibling lands immediately either
// way -- DeliverReport's conversation-append leg never waits on the sweep.
func (d *DelegationDelivery) NudgeUndrained(ctx context.Context, now time.Time, limit int) (int, error) {
	if d == nil || d.Nudge == nil || d.Channel == nil || d.NudgeAfter <= 0 {
		return 0, nil
	}
	cutoff := now.Add(-d.NudgeAfter)
	rows, err := d.Nudge.ListUnnudgedDelegationResults(ctx, cutoff, limit)
	if err != nil {
		return 0, fmt.Errorf("swarm: list unnudged delegation results: %w", err)
	}
	groups, err := groupByFanout(rows)
	if err != nil {
		return 0, fmt.Errorf("swarm: group delegation results: %w", err)
	}
	nudged := 0
	var errs []error
	for _, group := range groups {
		ok, err := d.nudgeFanout(ctx, group)
		if err != nil {
			errs = append(errs, fmt.Errorf("nudge fanout %s/%s: %w", group.identityID, group.fanoutKey, err))
			continue
		}
		if ok {
			nudged++
		}
	}
	return nudged, errors.Join(errs...)
}
