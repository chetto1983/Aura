// serve_delegation.go wires the SWARM-03/09 background delegation claim loop
// (plus its plan 51-10 delivery concern: the SC#1 conversation record, the
// present-operator steer push, and the absent-operator channel nudge) into the
// daemon composition root. Split from serve.go (boot/lifecycle) to keep it off
// the 600-LOC ceiling, mirroring serve_dispatch.go's own split rationale.
package main

import (
	"context"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/channels"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/runner"
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
// swarm-local conversation-scoped ChannelDeliverer seam —
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
		out = append(out, swarm.UndrainedResult{ID: r.ID, IdentityID: r.IdentityID, ConversationID: r.ConversationID, Body: r.Body, FanoutKey: r.FanoutKey})
	}
	return out, nil
}

// MarkFanoutNudged adapts the complete rows returned by the atomic claim. This is the
// same package-cycle boundary ListUnnudgedDelegationResults crosses above.
func (a steerNudgeAdapter) MarkFanoutNudged(ctx context.Context, identityID, fanoutKey string) ([]swarm.UndrainedResult, error) {
	rows, err := a.store.MarkFanoutNudged(ctx, identityID, fanoutKey)
	if err != nil {
		return nil, err
	}
	out := make([]swarm.UndrainedResult, 0, len(rows))
	for _, r := range rows {
		out = append(out, swarm.UndrainedResult{ID: r.ID, IdentityID: r.IdentityID, ConversationID: r.ConversationID, Body: r.Body, FanoutKey: r.FanoutKey})
	}
	return out, nil
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
		NotifyRoute:  string(cron.RouteStdout),
		Body:         body,
		Status:       "failed",
		LastError:    lastErr,
	})
	return err
}

var _ swarm.PendingNotificationStore = delegationPendingNotifier{}

// delegationReportArchiver adapts *assets.Service onto swarm.ReportArchiver
// (51-11, UI-SPEC §2) via the SAME IngestAgentFile seam send_file's own
// sendFileAssetAdapter uses -- no new artifact store, no new frame type
// (D-02's closed shape). ThreadID is the origin conversation id, so the
// shipped GET /api/assets?thread_id= the Artifacts panel already polls needs
// nothing new to find it.
type delegationReportArchiver struct{ svc *assets.Service }

var _ swarm.ReportArchiver = delegationReportArchiver{}

func (a delegationReportArchiver) ArchiveReport(ctx context.Context, identityID, conversationID, filename, markdown string) (string, error) {
	asset, err := a.svc.IngestAgentFile(ctx, assets.AgentIngestRequest{
		IdentityID: identityID,
		ThreadID:   conversationID,
		FileName:   filename,
		MIMEType:   "text/markdown",
		SizeBytes:  int64(len(markdown)),
		Reader:     strings.NewReader(markdown),
	})
	if err != nil {
		return "", err
	}
	return asset.ID, nil
}

// delegationJobCounterAdapter adapts *documents.PostgresIngestionJobStore onto
// swarm.FanoutJobCounter (51-11 Task 3) via CountUnfinishedDelegationJobs -- the same
// lightweight per-function store construction newRuntimeDelegationWorker already does
// (NewPostgresIngestionJobStore wraps a pool; it opens no new connection).
type delegationJobCounterAdapter struct {
	store *documents.PostgresIngestionJobStore
}

func (a delegationJobCounterAdapter) CountUnfinishedDelegationJobs(ctx context.Context, identityID, fanoutKey string) (int, error) {
	return a.store.CountUnfinishedDelegationJobs(ctx, identityID, fanoutKey)
}

var _ swarm.FanoutJobCounter = delegationJobCounterAdapter{}

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

// newDelegationDelivery builds the plan 51-10/51-11 delivery concern from the
// live runtime: the SC#1 conversation record, the unchanged present-operator
// steer push (D-04), the absent-operator channel nudge (D-02), (51-11) the
// report archive, and (51-11 Task 3) the fan-out eligibility counter. A nil
// chat.steer (no Postgres configured), a nil reg, a nil chat.assets, or a nil
// chat.pool degrades those legs to a no-op/no-artifact-pointer/always-eligible
// rather than dereferencing — Deliver/DeliverReport's own nil-receiver guard
// on Recorder is the hard stop (a wiring bug, not a domain rejection).
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
	var archiver swarm.ReportArchiver
	if chat.assets != nil {
		archiver = delegationReportArchiver{svc: chat.assets}
	}
	var counter swarm.FanoutJobCounter
	if chat.pool != nil {
		counter = delegationJobCounterAdapter{store: documents.NewPostgresIngestionJobStore(chat.pool)}
	}
	return swarm.NewDelegationDelivery(
		delegationConversationRecorder{store: chat.conv},
		steerPub,
		channel,
		nudge,
		delegationPendingNotifier{store: store},
		archiver,
		counter,
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
		Runtime:        chat.llmRuntime,
		Cfg:            *chat.cfg,
		ParentRegistry: chat.reg,
		Gateway:        chat.gateway,
	}
	// pauseParker is 51-06b Task 1's one-transaction seam: a claimed job's
	// AwaitingInput report opens its own attributed pause AND parks its row
	// atomically (delegation_pause_committer.go), mirroring
	// runner.PoolResumeCommitter's own cross-store tx composition.
	pauseParker := newDelegationPauseCommitter(chat.pool, chat.pause, store)
	claimLoop := &runtimeTenantIngestionProcessor{
		identities:     identity.New(chat.pool),
		width:          1,
		workerIDPrefix: runtimeDelegationWorkerIDPrefix,
		worker: func(identityID, workerID string) runtimeIngestionProcessor {
			loop := swarm.NewDelegationClaimLoop(store, delivery, pauseParker, identityID, workerTemplate, leaseDuration, pollInterval)
			loop.WorkerID = workerID
			return loop
		},
	}
	// resumeObserver is 51-06b Task 2: it watches for delegation pauses that have been
	// answered through the SAME generic /api/approvals -> Runner.SubmitAnswer bridge
	// every other pause resolves through, and returns their parked queue row to
	// claimable exactly once -- it does not answer pauses and does not run the worker,
	// riding the SAME runtimeTenantIngestionProcessor identity-enumeration wrapper as
	// the claim loop above (no second scheduler).
	resumeObserver := &runtimeTenantIngestionProcessor{
		identities:     identity.New(chat.pool),
		width:          1,
		workerIDPrefix: runtimeDelegationWorkerIDPrefix + "-resume",
		worker: func(identityID, workerID string) runtimeIngestionProcessor {
			return delegationResumeObserverAdapter{
				observer:   swarm.NewDelegationResumeObserver(store),
				identityID: identityID,
			}
		},
	}
	// pauseSweep is 51-06b Task 3: it expires background-worker pauses past
	// AURA_ASKUSER_PAUSE_TTL_SEC and takes their parked queue row with them (D-08
	// extended to the queue row), riding the SAME identity-enumeration wrapper as the
	// claim loop and resume observer above -- one lifecycle, one shutdown-cancellable
	// context, no second scheduler. Its tick is approvalExpiryInterval(ttl), the SAME
	// cadence the approval sweep derives from the same knob; a ttl <= 0 yields interval
	// 0, which runtimeIngestionWorker.Start treats as "never start" -- the shipped
	// "TTL <= 0 disables expiry" precedent, reached through the existing guard.
	pauseTTL := time.Duration(chat.cfg.AskUser.PauseTTLSec) * time.Second
	pauseExpirer := runner.NewPoolWorkerPauseExpirer(chat.pool, chat.conv, chat.pause, workerPauseQueueAdapter{store: store})
	pauseSweep := &runtimeTenantIngestionProcessor{
		identities:     identity.New(chat.pool),
		width:          1,
		workerIDPrefix: runtimeDelegationWorkerIDPrefix + "-pause-sweep",
		worker: func(identityID, _ string) runtimeIngestionProcessor {
			return workerPauseSweepAdapter{
				run:        chat.run,
				identityID: identityID,
				ttl:        pauseTTL,
				deps:       runner.WorkerPauseSweepDeps{Lister: workerPauseListerAdapter{store: store}, Expirer: pauseExpirer},
			}
		},
	}
	return &runtimeProcessingWorkers{workers: []*runtimeIngestionWorker{
		newRuntimeIngestionWorker(claimLoop, pollInterval),
		newRuntimeIngestionWorker(resumeObserver, pollInterval),
		newRuntimeIngestionWorker(&delegationNudgeProcessor{delivery: delivery}, pollInterval),
		newRuntimeIngestionWorker(pauseSweep, approvalExpiryInterval(pauseTTL)),
	}}
}

// runtimeWorkerPauseSweepBatchSize bounds one ExpireWorkerPauses pass per identity
// (51-06b Task 3) -- the same width the resume observer uses.
const runtimeWorkerPauseSweepBatchSize = runtimeDelegationNudgeBatchSize

// workerPauseSweepAdapter binds *runner.Runner.ExpireWorkerPauses' explicit-identity,
// explicit-deps signature onto the ctx-only runtimeIngestionProcessor shape
// runtimeTenantIngestionProcessor's per-identity worker factory expects -- mirroring
// delegationResumeObserverAdapter's own binding shape below.
type workerPauseSweepAdapter struct {
	run        *runner.Runner
	identityID string
	ttl        time.Duration
	deps       runner.WorkerPauseSweepDeps
}

func (a workerPauseSweepAdapter) ProcessOnce(ctx context.Context) (int, error) {
	return a.run.ExpireWorkerPauses(ctx, a.deps, a.identityID, time.Now(), a.ttl, runtimeWorkerPauseSweepBatchSize)
}

// workerPauseListerAdapter adapts *documents.PostgresIngestionJobStore.
// ListExpiredAwaitingInput onto runner.WorkerPauseLister: the two packages declare
// their row types independently (internal/runner must not import internal/documents
// for one sweep), so this is the ONE translation, a sibling of steerNudgeAdapter.
// OwningWorkerID is the job id by construction (51-06b Task 1 parks with
// OwningWorkerID = job.ID), so the mapping restates that rather than re-reading it.
type workerPauseListerAdapter struct {
	store *documents.PostgresIngestionJobStore
}

func (a workerPauseListerAdapter) ListExpiredWorkerPauses(ctx context.Context, identityID string, cutoff time.Time, limit int) ([]runner.ExpiredWorkerPause, error) {
	rows, err := a.store.ListExpiredAwaitingInput(ctx, identityID, cutoff, limit)
	if err != nil {
		return nil, err
	}
	out := make([]runner.ExpiredWorkerPause, 0, len(rows))
	for _, r := range rows {
		out = append(out, runner.ExpiredWorkerPause{
			JobID:      r.JobID,
			IdentityID: r.IdentityID,
			Pause: askuser.Pending{
				Token:           r.PauseToken,
				ConversationID:  r.ConversationID,
				Kind:            r.Kind,
				Question:        r.Question,
				ToolCallID:      r.ToolCallID,
				PendingActionID: r.PendingActionID,
				OwningWorkerID:  r.JobID,
			},
		})
	}
	return out, nil
}

var _ runner.WorkerPauseLister = workerPauseListerAdapter{}

// workerPauseQueueAdapter adapts the Tx-bound
// *documents.PostgresIngestionJobStore.ResolveIngestionJobAwaitingInputTx onto
// runner.WorkerPauseQueueResolver, so PoolWorkerPauseExpirer resolves the parked row
// on ITS transaction (the pause claim, the trace and the row die together, D-08).
type workerPauseQueueAdapter struct {
	store *documents.PostgresIngestionJobStore
}

func (a workerPauseQueueAdapter) ResolveAwaitingInputTx(ctx context.Context, q *sqlc.Queries, identityID, jobID, errorMessage string) (int64, error) {
	return a.store.ResolveIngestionJobAwaitingInputTx(ctx, q, documents.ResolveAwaitingInputRequest{
		IdentityID: identityID, JobID: jobID, ErrorMessage: errorMessage,
	})
}

var _ runner.WorkerPauseQueueResolver = workerPauseQueueAdapter{}

// delegationResumeObserverAdapter binds *swarm.DelegationResumeObserver's explicit-
// identity ProcessOnce(ctx, identityID, limit) onto the ctx-only runtimeIngestionProcessor
// shape runtimeTenantIngestionProcessor's per-identity worker factory expects.
type delegationResumeObserverAdapter struct {
	observer   *swarm.DelegationResumeObserver
	identityID string
}

func (a delegationResumeObserverAdapter) ProcessOnce(ctx context.Context) (int, error) {
	return a.observer.ProcessOnce(ctx, a.identityID, runtimeDelegationNudgeBatchSize)
}
