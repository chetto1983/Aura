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

	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identityctx"
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
	// Resume is the 51-06b continuation snapshot (D-01: no new table, the queue row's
	// EXISTING payload carries it): nil on every ordinary delegation that has never
	// paused; set by Task 1 the moment a worker's report comes back
	// StatusNeedsUserInput, and completed with the operator's answer by the resume
	// observer immediately before un-parking. Its presence, not a separate status flag,
	// is what tells processJob to rebuild through runChild with ResumeTurns instead of
	// a fresh brief.
	Resume *DelegationResumeState `json:"resume,omitempty"`
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
	// ListAnsweredAwaitingInput + UnparkIngestionJob back DelegationResumeObserver
	// (delegation_resume.go, 51-06b Task 2): which parked jobs may now resume, and
	// returning one to claimable exactly once. *documents.PostgresIngestionJobStore
	// satisfies both by construction, alongside the five methods above.
	ListAnsweredAwaitingInput(ctx context.Context, identityID string, limit int) ([]documents.AnsweredAwaitingInputJob, error)
	UnparkIngestionJob(ctx context.Context, req documents.UnparkIngestionJobRequest) (int64, error)
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
	Store DelegationJobStore
	// Delivery is the SC#1 conversation record + present-operator steer push +
	// absent-operator channel nudge (plan 51-10, delegation_delivery.go). A nil
	// Delivery degrades deliverSuccess to a hard config error (Deliver's own
	// nil-receiver guard) rather than silently skipping the SC#1 write.
	Delivery      *DelegationDelivery
	IdentityID    string
	WorkerID      string
	LeaseDuration time.Duration
	PollInterval  time.Duration
	BatchSize     int
	RetryBackoff  time.Duration
	Worker        RunConfig
	// PauseParker is the 51-06b Task 1 seam: a claimed job's AwaitingInput report opens
	// its own attributed pause and parks its row in ONE transaction (delegation_resume.go).
	// A nil PauseParker degrades a StatusNeedsUserInput report to a hard config error
	// (openPauseAndPark's own guard), never a silent skip -- mirrors Delivery's own
	// nil-receiver posture for the SC#1 write.
	PauseParker PauseAndPark
}

// NewDelegationClaimLoop builds a claim loop bound to one identity's queue.
func NewDelegationClaimLoop(store DelegationJobStore, delivery *DelegationDelivery, pauseParker PauseAndPark, identityID string, worker RunConfig, leaseDuration, pollInterval time.Duration) *DelegationClaimLoop {
	return &DelegationClaimLoop{
		Store:         store,
		Delivery:      delivery,
		PauseParker:   pauseParker,
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
	// The ONE place a claimed row's identity is bound onto ctx. The claim loop's own
	// ctx (ProcessOnce's daemon background loop, never a per-request context) carries
	// no identityctx, and every per-identity write downstream reads it: the worker's
	// own identity-scoped tools (delegationOperationContext), Deliver's
	// ConversationRecorder (conversations.Store.AppendTurn sets the RLS carrier from
	// identityctx.IdentityID), and the pause-and-park question delivery. Measured live
	// 2026-08-29 (live-check/d03/RESULTS.md defect A) when only the worker had the bind:
	// every report failed "conversation not found", the queue re-ran the WHOLE worker
	// per attempt and dead-lettered. Binding per call site fixed it and was then
	// consolidated here the same day -- one boundary, no site left to forget.
	ctx = identityctx.WithIdentityID(ctx, job.IdentityID)

	payload, err := delegationPayloadFromJob(job)
	if err != nil {
		return l.recordFailure(ctx, job, fmt.Errorf("delegation payload: %w", err))
	}

	// 51-06b (T-51-36, D-00's second LibreChat trap): a resume state whose
	// AgentIdentity does not match the identity this job is claimed under refuses
	// loudly, runs nothing -- never a silent rebuild under someone else's registry,
	// gateway or budget. Checked BEFORE runWithHeartbeat so a mismatch never
	// constructs a worker at all (runWithHeartbeat's own error return is reserved for
	// "lease lost mid-run", which must NOT record a failure -- a distinct case this
	// check keeps separate).
	if payload.Resume != nil && payload.Resume.AgentIdentity != job.IdentityID {
		return l.recordFailure(ctx, job, fmt.Errorf(
			"delegation resume: agent identity mismatch (state=%s job=%s) -- refusing to rebuild",
			payload.Resume.AgentIdentity, job.IdentityID))
	}

	report, history, err := l.runWithHeartbeat(ctx, job, payload)
	if err != nil {
		// Lease lost mid-run: another worker now owns this row. Writing any
		// further state here would race that worker's own transition.
		return err
	}

	switch report.Status {
	case StatusOK:
		return l.deliverSuccess(ctx, job, payload, report)
	case StatusNeedsUserInput:
		// 51-06b Task 1/2: the worker opens its OWN attributed, fenced pause and
		// parks its row instead of failing -- the resumable path this phase closes.
		return l.openPauseAndPark(ctx, job, payload, report, history)
	default:
		msg := report.Error
		if msg == "" {
			msg = "worker failed"
		}
		return l.recordFailure(ctx, job, errors.New(msg))
	}
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

// deliverSuccess runs the claimed job's report through Delivery.Deliver --
// the SC#1 conversation record BEFORE the present-operator steer push (plan
// 51-10) -- and gates the row's succeeded transition on the record having
// actually succeeded, never on the push alone. A push (steer publish)
// INFRASTRUCTURE failure propagates as a hard error exactly as before this
// plan (51-01's original contract, unchanged); a RECORD failure is not a hard
// error (Deliver WARNs and returns recorded=false) but must still stop the
// row from being marked succeeded -- a report that reached neither the
// conversation nor (once nudged) a channel has not been delivered, so it is
// retried by the shipped attempt_count/next_attempt_at backoff instead.
func (l *DelegationClaimLoop) deliverSuccess(ctx context.Context, job documents.IngestionJob, payload DelegationPayload, report ChildReport) error {
	text, err := marshalReports([]ChildReport{report})
	if err != nil {
		return fmt.Errorf("delegation report marshal: %w", err)
	}
	// ctx already carries the job's identity (bound once in processJob); Deliver's
	// ConversationRecorder reads it to scope the RLS write.
	recorded, err := l.Delivery.Deliver(ctx, payload, text)
	if err != nil {
		return fmt.Errorf("delegation report deliver: %w", err)
	}
	if !recorded {
		return l.recordFailure(ctx, job, fmt.Errorf(
			"delegation report was not recorded to its origin conversation %s", payload.ConversationID))
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
// is asserted >= this same floor by config_test.go. D-03's termination model
// (plan 51-09) reaps a worker on INACTIVITY (AURA_SWARM_CHILD_IDLE_SEC,
// default 120), not on age -- config.GuardSwarmStaleness refuses to boot
// unless the idle deadline is strictly shorter than this lease, so a lease
// reclaim can never race a goroutine that is still alive.
const defaultDelegationLeaseSec = 300
