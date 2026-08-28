// serve_delegation.go wires the SWARM-03/09 background delegation claim loop
// (plus its plan 51-10 delivery concern: the SC#1 conversation record, the
// present-operator steer push, and the absent-operator channel nudge) into the
// daemon composition root. Split from serve.go (boot/lifecycle) to keep it off
// the 600-LOC ceiling, mirroring serve_dispatch.go's own split rationale.
package main

import (
	"context"
	"time"

	"github.com/chetto1983/aura/internal/channels"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/steer"
	"github.com/chetto1983/aura/internal/swarm"
)

// runtimeDelegationWorkerIDPrefix names the delegation claim loop's per-identity
// worker id, distinct from the asset-processing worker's own prefix so a
// locked_by column value unambiguously identifies which loop holds a lease.
const runtimeDelegationWorkerIDPrefix = "aura-swarm-delegation"

// runtimeDelegationNudgeBatchSize bounds one NudgeUndrained pass. The nudge
// sweep is system-wide (unscoped by identity, mirroring the steer TTL sweep),
// NOT a per-tenant request, so it is not scaled by identity count the way the
// claim loop's per-identity worker width is.
const runtimeDelegationNudgeBatchSize = 20

// var _ asserts at the composition root that *channels.Registry satisfies the
// swarm-local ChannelDeliverer seam (via its 20-01 DeliverToIdentity method) —
// the assertion lives in cmd/aura, NOT in swarm (swarm must not import channels).
var _ swarm.ChannelDeliverer = (*channels.Registry)(nil)

// delegationConversationRecorder adapts *conversations.Store onto
// swarm.ConversationRecorder — the SAME out-of-band append
// serve_dispatch.go's own conversationRecorder makes for the scheduler (Seq 0:
// "the scheduler is not inside the turn loop that would otherwise be
// numbering these" applies identically to a claim loop's worker, one layer
// further out). A distinct type from serve_dispatch.go's conversationRecorder
// keeps the two composition-root concerns independently readable.
type delegationConversationRecorder struct {
	store *conversations.Store
}

func (r delegationConversationRecorder) AppendAssistantTurn(ctx context.Context, conversationID, text string) error {
	if r.store == nil {
		return nil
	}
	return r.store.AppendTurn(ctx, conversations.AppendTurnParams{
		ConversationID: conversationID,
		Seq:            0,
		Role:           "assistant",
		Content:        text,
	})
}

var _ swarm.ConversationRecorder = delegationConversationRecorder{}

// steerNudgeAdapter adapts *steer.PostgresStore onto swarm.SteerNudgeStore:
// the two packages declare structurally-identical row types independently
// (internal/steer must never import internal/swarm, which already imports
// internal/steer for steer.SourceWorker, so a shared type would cycle). This
// is the ONE translation layer the delegation delivery wiring needs — every
// other seam (ConversationRecorder above, ChannelDeliverer, SteerPublisher) is
// satisfied by construction.
type steerNudgeAdapter struct {
	store *steer.PostgresStore
}

func (a steerNudgeAdapter) ListUnnudgedDelegationResults(ctx context.Context, cutoff time.Time, limit int) ([]swarm.UndrainedResult, error) {
	rows, err := a.store.ListUnnudgedDelegationResults(ctx, cutoff, limit)
	if err != nil {
		return nil, err
	}
	out := make([]swarm.UndrainedResult, 0, len(rows))
	for _, r := range rows {
		out = append(out, swarm.UndrainedResult{ID: r.ID, IdentityID: r.IdentityID, Body: r.Body})
	}
	return out, nil
}

func (a steerNudgeAdapter) MarkSteerRowNudged(ctx context.Context, id, identityID string) (bool, error) {
	return a.store.MarkSteerRowNudged(ctx, id, identityID)
}

var _ swarm.SteerNudgeStore = steerNudgeAdapter{}

// delegationPendingNotifier adapts *cron.Store onto
// swarm.PendingNotificationStore for the absent-operator leg's owns-but-
// failed retry (migration 0105's steer_queue_id leg) — the SAME store
// internal/cron/deliver.go's own owns-but-failed leg already writes through
// for a scheduled task, never a second outbox (D-02).
type delegationPendingNotifier struct {
	store *cron.Store
}

func (n delegationPendingNotifier) InsertPendingNotification(ctx context.Context, steerQueueID, identityID, body, lastErr string) error {
	if n.store == nil {
		return nil
	}
	_, err := n.store.InsertPendingNotification(ctx, cron.InsertPendingNotificationParams{
		SteerQueueID: steerQueueID,
		IdentityID:   identityID,
		Body:         body,
		Status:       "failed",
		LastError:    lastErr,
	})
	return err
}

var _ swarm.PendingNotificationStore = delegationPendingNotifier{}

// delegationNudgeProcessor adapts DelegationDelivery.NudgeUndrained onto
// runtimeIngestionProcessor so it rides the SAME runtimeProcessingWorkers
// container (Start/Stop, same context) the claim loop already uses — no
// second scheduler, per this plan's own prohibition.
type delegationNudgeProcessor struct {
	delivery *swarm.DelegationDelivery
}

func (p *delegationNudgeProcessor) ProcessOnce(ctx context.Context) (int, error) {
	return p.delivery.NudgeUndrained(ctx, time.Now(), runtimeDelegationNudgeBatchSize)
}

// newDelegationDelivery builds the plan 51-10 delivery concern from the live
// runtime: the SC#1 conversation record, the unchanged present-operator steer
// push (D-04), and the absent-operator channel nudge (D-02). A nil chat.steer
// (no Postgres configured) or a nil reg degrades those legs to a no-op rather
// than dereferencing — Deliver's own nil-receiver guard on Recorder is the
// hard stop (a wiring bug, not a domain rejection).
func newDelegationDelivery(chat *chatEnv, store *cron.Store, reg *channels.Registry) *swarm.DelegationDelivery {
	var steerPub swarm.SteerPublisher
	var nudge swarm.SteerNudgeStore
	if chat.steer != nil {
		steerPub = chat.steer
		nudge = steerNudgeAdapter{store: chat.steer}
	}
	var channel swarm.ChannelDeliverer
	if reg != nil {
		channel = reg
	}
	return swarm.NewDelegationDelivery(
		delegationConversationRecorder{store: chat.conv},
		steerPub,
		channel,
		nudge,
		delegationPendingNotifier{store: store},
		time.Duration(chat.cfg.SwarmDelegationNudgeSec)*time.Second,
	)
}

// newRuntimeDelegationWorker builds the resident delegation claim loop plus
// the absent-operator nudge sweep on the SAME runtimeProcessingWorkers
// container (same context, same shutdown — no second scheduler), reusing
// runtimeTenantIngestionProcessor's identity-enumeration wrapper VERBATIM
// (asset_processing_worker.go) so the SAME "one poll, every active identity"
// shape drains the delegation queue instead of a single hardcoded identity —
// this daemon may be multi-tenant (Authula). worker is the static per-daemon
// RunConfig template (client/LLM/registry/gateway/Cfg) runChild needs; ConvID
// and Depth are overridden per claimed job from its payload
// (DelegationClaimLoop.runWithHeartbeat).
//
// delivery carries the conversation record, present-operator steer push, and
// absent-operator channel nudge (plan 51-10, newDelegationDelivery). A nil
// pool (no Postgres configured) yields a no-op set of workers, matching
// newRuntimeAssetProcessingWorker's own degrade.
func newRuntimeDelegationWorker(chat *chatEnv, delivery *swarm.DelegationDelivery) *runtimeProcessingWorkers {
	if chat == nil || chat.pool == nil || chat.cfg == nil {
		return &runtimeProcessingWorkers{}
	}
	store := documents.NewPostgresIngestionJobStore(chat.pool)
	leaseDuration := time.Duration(chat.cfg.SwarmDelegationLeaseSec) * time.Second
	pollInterval := time.Duration(chat.cfg.SwarmDelegationPollSec) * time.Second
	workerTemplate := swarm.RunConfig{
		Client:         chat.client,
		LLM:            chat.cfg.LLM,
		Cfg:            *chat.cfg,
		ParentRegistry: chat.reg,
		Gateway:        chat.gateway,
	}
	claimLoop := &runtimeTenantIngestionProcessor{
		identities:     identity.New(chat.pool),
		width:          1,
		workerIDPrefix: runtimeDelegationWorkerIDPrefix,
		worker: func(identityID, workerID string) runtimeIngestionProcessor {
			loop := swarm.NewDelegationClaimLoop(store, delivery, identityID, workerTemplate, leaseDuration, pollInterval)
			loop.WorkerID = workerID
			return loop
		},
	}
	return &runtimeProcessingWorkers{workers: []*runtimeIngestionWorker{
		newRuntimeIngestionWorker(claimLoop, pollInterval),
		newRuntimeIngestionWorker(&delegationNudgeProcessor{delivery: delivery}, pollInterval),
	}}
}
