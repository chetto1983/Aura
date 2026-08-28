// Durable background delegation (SWARM-03/09): a top-level swarm_spawn call
// stops holding the operator's turn hostage by writing one durable row per
// goal into the GENERALIZED aura.ingestion_jobs queue (D-01, spike 100) rather
// than a new table, and a daemon-resident claim loop drains it through the
// REAL ClaimIngestionJobs Go path (jobs_worker.go's proven shape) calling the
// SAME runChild every synchronous worker uses -- one worker construction, one
// event loop, so this new path cannot drift from runChild's own
// delegated-dispatch-reservation fix (791dcd7e0) or its idempotency guard
// (67d24aee4). The consolidated report re-enters the conversation via the
// shipped steer rail under steer.SourceWorker (D-04), never a second delivery
// mechanism or envelope.
package swarm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/steer"
)

// JobTypeSwarmDelegation is the aura.ingestion_jobs discriminator for a
// background delegation row (D-01). The shipped CHECK vocabulary on `status`
// (queued, running, succeeded, failed, dead_letter, canceled -- migration
// 0025) is reused verbatim; there is no "completed" status.
const JobTypeSwarmDelegation = "swarm_delegation"

// Delegation queue defaults. A delegation attempt is a full LLM worker turn,
// not a cheap pipeline stage, so the retry budget is deliberately smaller than
// document ingestion's default of 5.
const (
	defaultDelegationMaxAttempts  = 3
	defaultDelegationWorkerID     = "aura-swarm-delegation-worker"
	defaultDelegationBatchSize    = 4
	defaultDelegationPollInterval = 2 * time.Second
	defaultDelegationRetryBackoff = 15 * time.Second
)

// DelegationPayload is the aura.ingestion_jobs.payload shape for a
// job_type=swarm_delegation row. Context is carried for forward-compat with
// SWARM-01's goal/context split (plan 51-03); until that split lands, the
// full brief travels in Goal and Context stays empty.
type DelegationPayload struct {
	Goal           string `json:"goal"`
	Context        string `json:"context,omitempty"`
	ConversationID string `json:"conversation_id"`
	ParentRunID    string `json:"parent_run_id,omitempty"`
	Depth          int    `json:"depth"`
}

// SteerPublisher is the narrow steer-push seam the claim loop needs to
// deliver a consolidated worker report (D-04). Declared HERE (the consuming
// package), never importing a concrete steer type directly -- the same shape
// as SteerInbox (internal/agent/llm_agent_steer.go) and ChannelDeliverer
// (internal/cron/deliver.go). *steer.Inbox satisfies this by construction.
type SteerPublisher interface {
	Push(conv, source, text string) error
}

// DelegationJobStore is the seam over the shared Postgres ingestion-job queue
// (documents.PostgresIngestionJobStore) the enqueue + claim loop need.
// Declared HERE so internal/swarm depends on internal/documents' concrete
// request/row types (reusing the shipped lease/fence/backoff engine, D-01)
// without internal/documents ever importing swarm.
type DelegationJobStore interface {
	Create(ctx context.Context, req documents.CreateIngestionJobRequest) (documents.IngestionJob, error)
	Claim(ctx context.Context, req documents.ClaimIngestionJobsRequest) ([]documents.IngestionJob, error)
	UpdateStatus(ctx context.Context, req documents.TransitionIngestionJobRequest) (documents.IngestionJob, error)
	Retry(ctx context.Context, req documents.RetryIngestionJobRequest) (documents.IngestionJob, error)
	Heartbeat(ctx context.Context, req documents.HeartbeatIngestionJobRequest) (documents.IngestionJob, error)
}

// DelegationEnqueuer writes one durable job_type=swarm_delegation row per
// goal. A zero-value Store is a wiring bug (real Go error), never a domain
// rejection.
type DelegationEnqueuer struct {
	Store DelegationJobStore
}

// EnqueueDelegation writes one durable row per goal and returns a
// model-readable summary immediately -- no worker is constructed here (the
// claim loop does that out of band). An empty goal slice is a model-readable
// rejection, not a Go error (D-15's established domain-rejection idiom), so
// zero rows are enqueued and the model can self-correct without a stack
// trace.
func EnqueueDelegation(ctx context.Context, enq *DelegationEnqueuer, identityID string, goals []string, brief DelegationPayload) (string, error) {
	if len(goals) == 0 {
		return "error: no goals provided -- background delegation needs at least one goal", nil
	}
	if enq == nil || enq.Store == nil {
		return "", fmt.Errorf("swarm: delegation enqueuer is not configured")
	}
	queued := 0
	for i, goal := range goals {
		key := delegationIdempotencyKey(identityID, brief.ConversationID, brief.ParentRunID, i, goal)
		payload := brief
		payload.Goal = goal
		m, err := delegationPayloadMap(payload)
		if err != nil {
			return "", fmt.Errorf("swarm: delegation payload for goal %d: %w", i, err)
		}
		if _, err := enq.Store.Create(ctx, documents.CreateIngestionJobRequest{
			IdentityID:     identityID,
			JobType:        JobTypeSwarmDelegation,
			Status:         "queued",
			IdempotencyKey: key,
			MaxAttempts:    defaultDelegationMaxAttempts,
			Payload:        m,
		}); err != nil {
			return "", fmt.Errorf("swarm: enqueue delegation goal %d: %w", i, err)
		}
		queued++
	}
	return fmt.Sprintf(
		"queued: %d worker(s) dispatched in the background; you can keep talking, results will arrive as they complete",
		queued), nil
}

// delegationIdempotencyKey is deterministic over its inputs: the same
// (identity, conversation, parent run, goal index, goal text) always produces
// the same key, and a different goal index always produces a different one
// (the ON CONFLICT (identity_id, job_type, idempotency_key) unique key is what
// makes a re-run of the same enqueue add no second row).
func delegationIdempotencyKey(identityID, convID, parentRunID string, goalIndex int, goal string) string {
	h := sha256.New()
	for _, part := range []string{identityID, convID, parentRunID, strconv.Itoa(goalIndex), goal} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return "swarm_delegation:" + hex.EncodeToString(h.Sum(nil))
}

func delegationPayloadMap(p DelegationPayload) (map[string]any, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func delegationPayloadFromJob(job documents.IngestionJob) (DelegationPayload, error) {
	b, err := json.Marshal(job.Payload)
	if err != nil {
		return DelegationPayload{}, fmt.Errorf("delegation payload encode: %w", err)
	}
	var p DelegationPayload
	if err := json.Unmarshal(b, &p); err != nil {
		return DelegationPayload{}, fmt.Errorf("delegation payload decode: %w", err)
	}
	if p.Goal == "" {
		return DelegationPayload{}, errors.New("delegation payload missing goal")
	}
	if p.ConversationID == "" {
		return DelegationPayload{}, errors.New("delegation payload missing conversation_id")
	}
	return p, nil
}

// DelegationClaimLoop is the daemon-resident claim loop for
// job_type=swarm_delegation rows (SWARM-09). Worker is the static
// per-identity construction template (client, LLM config, parent registry,
// gateway, Cfg) runChild needs; ConvID and Depth are overridden per claimed
// job from its payload.
type DelegationClaimLoop struct {
	Store         DelegationJobStore
	Steer         SteerPublisher
	IdentityID    string
	WorkerID      string
	LeaseDuration time.Duration
	PollInterval  time.Duration
	BatchSize     int
	RetryBackoff  time.Duration
	Worker        RunConfig
}

// NewDelegationClaimLoop builds a claim loop bound to one identity's queue.
func NewDelegationClaimLoop(store DelegationJobStore, steerPub SteerPublisher, identityID string, worker RunConfig, leaseDuration, pollInterval time.Duration) *DelegationClaimLoop {
	return &DelegationClaimLoop{
		Store:         store,
		Steer:         steerPub,
		IdentityID:    identityID,
		LeaseDuration: leaseDuration,
		PollInterval:  pollInterval,
		Worker:        worker,
	}
}

// ProcessOnce claims one batch of due job_type=swarm_delegation rows (the
// query filters at claim time -- an ingestion row is never claimed, and never
// stolen from the document ingestion worker's own claim loop) and runs each
// to a terminal state. It mirrors IngestionJobWorker.ProcessOnce's shape
// (jobs_worker.go) verbatim.
func (l *DelegationClaimLoop) ProcessOnce(ctx context.Context) (int, error) {
	if l == nil || l.Store == nil {
		return 0, fmt.Errorf("delegation claim loop has no store")
	}
	if l.IdentityID == "" {
		return 0, fmt.Errorf("delegation claim loop has no identity")
	}
	jobs, err := l.Store.Claim(ctx, documents.ClaimIngestionJobsRequest{
		IdentityID:    l.IdentityID,
		JobType:       JobTypeSwarmDelegation,
		WorkerID:      l.workerID(),
		LeaseDuration: l.leaseDuration(),
		BatchSize:     l.batchSize(),
	})
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, job := range jobs {
		if job.IdentityID != l.IdentityID {
			return processed, fmt.Errorf("claimed delegation job %q has unexpected identity %q", job.ID, job.IdentityID)
		}
		if job.JobType != JobTypeSwarmDelegation {
			return processed, fmt.Errorf("claimed delegation job %q has unexpected job_type %q", job.ID, job.JobType)
		}
		if err := l.processJob(ctx, job); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

// Run polls ProcessOnce until ctx is cancelled. A pass error is logged and
// swallowed -- one bad pass never kills the daemon-resident loop, mirroring
// internal/cron's own "log and continue" lifecycle discipline.
func (l *DelegationClaimLoop) Run(ctx context.Context) error {
	ticker := time.NewTicker(l.pollInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if _, err := l.ProcessOnce(ctx); err != nil {
				slog.Warn("swarm.delegation.process_once_failed", "err", err)
			}
		}
	}
}

func (l *DelegationClaimLoop) processJob(ctx context.Context, job documents.IngestionJob) error {
	payload, err := delegationPayloadFromJob(job)
	if err != nil {
		return l.recordFailure(ctx, job, fmt.Errorf("delegation payload: %w", err))
	}

	report, err := l.runWithHeartbeat(ctx, job, payload)
	if err != nil {
		// Lease lost mid-run: another worker now owns this row. Writing any
		// further state here would race that worker's own transition.
		return err
	}

	switch report.Status {
	case StatusOK:
		return l.deliverSuccess(ctx, job, payload, report)
	case StatusNeedsUserInput:
		// A persisted, resumable per-worker pause is plan 51-06b's work
		// (SWARM-06). At this wave a paused delegation fails loudly with the
		// question preserved in the error rather than implying it is handled.
		return l.recordFailure(ctx, job, fmt.Errorf(
			"worker paused for operator input (not yet handled by this phase's tracer -- see plan 51-06b): %s",
			report.Question))
	default:
		msg := report.Error
		if msg == "" {
			msg = "worker failed"
		}
		return l.recordFailure(ctx, job, errors.New(msg))
	}
}

// runWithHeartbeat runs the claimed job through the SAME runChild every
// synchronous swarm invocation uses -- no second worker construction, no
// second event loop, so Pitfall 2's fix (791dcd7e0, gateway.WithDelegatedDispatch)
// rides along automatically -- while a sibling goroutine renews the Postgres
// lease on a fixed interval, copying jobs_worker.go's
// handleWithHeartbeat/maintainLease shape (goroutine racing the handler,
// context.WithCancel, buffered result channel).
//
// INTERIM (D-03): the heartbeat tick source here is a fixed ticker, not
// runChild's own per-event loop. runChild is called as one opaque blocking
// unit precisely so its ONE event loop stays in swarm.go (this plan's own
// prohibition on cloning it); threading a per-event liveness hook through it
// is D-03's REAL fix and is plan 51-09's job (child_staleness.go), once the
// termination model itself changes from a wall-clock cap to inactivity-based
// (unbounded worker lifetime). A fixed ticker is safe here because the
// configured lease default (AURA_SWARM_DELEGATION_LEASE_SEC=300) already
// exceeds the measured worst-case worker lifetime -- SwarmChildTimeoutSec +
// LLMTotalTimeoutSec = 240s effective (spike 099) -- so a missed tick cannot
// expire the lease before the worker naturally terminates. This is an INTERIM
// bound, not this phase's answer to D-03.
func (l *DelegationClaimLoop) runWithHeartbeat(ctx context.Context, job documents.IngestionJob, payload DelegationPayload) (ChildReport, error) {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	heartbeatErr := make(chan error, 1)
	go func() {
		heartbeatErr <- l.maintainLease(runCtx, cancel, job)
	}()

	rc := l.Worker
	rc.ConvID = payload.ConversationID
	rc.Depth = payload.Depth
	rc.Context = payload.Context

	budget, err := agent.NewBudgetFromEnv()
	if err != nil {
		cancel()
		<-heartbeatErr
		return ChildReport{}, fmt.Errorf("delegation job budget: %w", err)
	}

	// Mint the ROOT operation context here, mirroring cron/dispatch.go's
	// scheduledOperationContext exactly: a worker's own mutating tool calls
	// (shell_exec, write_file, ...) are denied "operation context missing" by
	// gateway.beginOperation unless a trusted parent operation already sits on
	// ctx (internal/agent/idempotency_operation.go's deriveToolOperationContext
	// derives a CHILD from a parent -- it never mints a root). The HTTP ingress
	// mints one for a live turn and the scheduler mints one for a cron dispatch;
	// this claim loop is the third kind of trusted root and had none, which
	// measured as a 100% deterministic denial of every worker tool call on the
	// very first live delegation (the same shape as spike 099's Pitfall 1, one
	// layer further out). Keyed on job.ID + LeaseGeneration so a reclaimed/retried
	// attempt is a genuinely different operation -- never a replay of a dead
	// attempt's stale result, exactly like a scheduler reclaim's fresh RunID.
	delegationCtx, err := delegationOperationContext(runCtx, job, payload)
	if err != nil {
		cancel()
		<-heartbeatErr
		return ChildReport{}, fmt.Errorf("delegation operation context: %w", err)
	}

	report := runChild(delegationCtx, rc, budget, 0, payload.Goal)
	cancel()
	if hbErr := <-heartbeatErr; hbErr != nil {
		return ChildReport{}, hbErr
	}
	return report, nil
}

// delegationOperationContext mints the trusted root operation context a
// claimed delegation job's worker needs before it can dispatch ANY mutating
// tool (gateway.beginOperation denies "operation context missing" otherwise --
// see runWithHeartbeat's doc comment). It also binds identityctx.WithIdentityID
// so the worker's own identity-scoped tools (document_search, skill_manage,
// send_file_ingest, and a NESTED swarm_spawn once 51-05 lifts the registry
// restriction) resolve the SAME identity the row is scoped to, not an absent
// one -- mirroring cron/dispatch.go's scheduledOperationContext, one layer
// further out (a worker instead of a scheduled task).
func delegationOperationContext(ctx context.Context, job documents.IngestionJob, payload DelegationPayload) (context.Context, error) {
	fingerprint, err := idempotency.FingerprintTyped(struct {
		JobID    string `json:"job_id"`
		Goal     string `json:"goal"`
		ConvID   string `json:"conversation_id"`
		ParentID string `json:"parent_run_id"`
	}{JobID: job.ID, Goal: payload.Goal, ConvID: payload.ConversationID, ParentID: payload.ParentRunID})
	if err != nil {
		return nil, fmt.Errorf("delegation operation fingerprint: %w", err)
	}
	operation := idempotency.Operation{
		Key: idempotency.OperationKey{
			IdentityID: job.IdentityID,
			Scope:      idempotency.ScopeSwarmDelegation,
			// LeaseGeneration increments on every claim (including a reclaim after
			// a dead worker's lease expired), so a retried attempt is always a
			// DIFFERENT operation -- never a replay of a stale/abandoned attempt.
			Key: job.ID + ":" + strconv.FormatInt(job.LeaseGeneration, 10),
		},
		Fingerprint: fingerprint,
		Correlation: job.ID,
	}
	trusted := identityctx.WithIdentityID(ctx, job.IdentityID)
	operationCtx, err := idempotency.WithOperation(trusted, operation)
	if err != nil {
		return nil, fmt.Errorf("delegation operation: %w", err)
	}
	return operationCtx, nil
}

func (l *DelegationClaimLoop) maintainLease(ctx context.Context, cancel context.CancelFunc, job documents.IngestionJob) error {
	interval := l.leaseDuration() / 3
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			_, err := l.Store.Heartbeat(ctx, documents.HeartbeatIngestionJobRequest{
				IdentityID: job.IdentityID, JobID: job.ID, WorkerID: l.workerID(),
				LeaseGeneration: job.LeaseGeneration, LeaseDuration: l.leaseDuration(),
			})
			if err == nil {
				continue
			}
			if ctx.Err() != nil {
				return nil
			}
			cancel()
			return err
		}
	}
}

// deliverSuccess pushes the consolidated report under steer.SourceWorker
// (D-04) BEFORE transitioning the row -- the transition is gated on the push
// having been attempted, never the other way around, so a delivery failure
// never gets silently reported as succeeded. markSteer
// (internal/agent/llm_agent_steer.go) picks the untrusted-tool-output
// envelope for this Source; any other Source string falls through to the
// operator-authority envelope and the model discounts the report as a
// spoofing attempt (spike 098, Pitfall 5).
func (l *DelegationClaimLoop) deliverSuccess(ctx context.Context, job documents.IngestionJob, payload DelegationPayload, report ChildReport) error {
	text, err := marshalReports([]ChildReport{report})
	if err != nil {
		return fmt.Errorf("delegation report marshal: %w", err)
	}
	if l.Steer != nil {
		if err := l.Steer.Push(payload.ConversationID, steer.SourceWorker, text); err != nil {
			return fmt.Errorf("delegation report push: %w", err)
		}
	}
	if _, err := l.Store.UpdateStatus(ctx, documents.TransitionIngestionJobRequest{
		IdentityID: job.IdentityID, JobID: job.ID, WorkerID: l.workerID(),
		LeaseGeneration: job.LeaseGeneration, Status: "succeeded",
		EventType: "swarm_delegation.succeeded", EventMessage: "delegation delivered",
	}); err != nil {
		return fmt.Errorf("delegation succeed transition: %w", err)
	}
	return nil
}

// recordFailure reuses jobs_worker.go's recordHandlerFailure branch verbatim:
// retry with backoff below max_attempts, dead_letter at the cap. No second
// retry/backoff implementation.
func (l *DelegationClaimLoop) recordFailure(ctx context.Context, job documents.IngestionJob, cause error) error {
	message := cause.Error()
	if job.MaxAttempts > 0 && job.AttemptCount >= job.MaxAttempts {
		if _, err := l.Store.UpdateStatus(ctx, documents.TransitionIngestionJobRequest{
			IdentityID: job.IdentityID, JobID: job.ID, WorkerID: l.workerID(),
			LeaseGeneration: job.LeaseGeneration, Status: "dead_letter",
			ErrorCode: "handler_failed", ErrorMessage: message,
			EventType: "swarm_delegation.dead_letter", EventMessage: message,
		}); err != nil {
			return err
		}
		slog.Warn("swarm.delegation.dead_letter", "job_id", job.ID, "attempts", job.AttemptCount, "err", message)
		return nil
	}
	if _, err := l.Store.Retry(ctx, documents.RetryIngestionJobRequest{
		IdentityID: job.IdentityID, JobID: job.ID, WorkerID: l.workerID(),
		LeaseGeneration: job.LeaseGeneration, ErrorCode: "handler_failed",
		ErrorMessage: message, EventMessage: message,
		NextAttemptAt: time.Now().UTC().Add(l.retryBackoff()),
	}); err != nil {
		return err
	}
	return nil
}

func (l *DelegationClaimLoop) workerID() string {
	if l.WorkerID != "" {
		return l.WorkerID
	}
	return defaultDelegationWorkerID
}

func (l *DelegationClaimLoop) leaseDuration() time.Duration {
	if l.LeaseDuration > 0 {
		return l.LeaseDuration
	}
	return time.Duration(defaultDelegationLeaseSec) * time.Second
}

func (l *DelegationClaimLoop) pollInterval() time.Duration {
	if l.PollInterval > 0 {
		return l.PollInterval
	}
	return defaultDelegationPollInterval
}

func (l *DelegationClaimLoop) batchSize() int {
	if l.BatchSize > 0 {
		return l.BatchSize
	}
	return defaultDelegationBatchSize
}

func (l *DelegationClaimLoop) retryBackoff() time.Duration {
	if l.RetryBackoff > 0 {
		return l.RetryBackoff
	}
	return defaultDelegationRetryBackoff
}

// defaultDelegationLeaseSec is the fallback lease when a caller builds a
// DelegationClaimLoop without a config-derived LeaseDuration (e.g. a unit
// test). Production wiring (cmd/aura) always passes
// AURA_SWARM_DELEGATION_LEASE_SEC via config.Config, whose own default (300s)
// is asserted >= this same floor by config_test.go. 240s is the measured
// effective worst-case worker lifetime (SwarmChildTimeoutSec +
// LLMTotalTimeoutSec, spike 099, D-03) -- this constant MUST NOT be sized
// against the nominal 120s alone, or a live worker's lease could be reclaimed
// out from under it.
const defaultDelegationLeaseSec = 300
