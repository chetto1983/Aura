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
	"errors"
	"fmt"
	"log/slog"
	"sync"
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
	// ChildID/GoalIndex/FanoutKey (51-11) ride the payload JSONB -- no migration
	// for any of them. ChildID is this goal's deterministic, stable worker id
	// (delegationChildID), minted once at EnqueueDelegation time and carried
	// unchanged through every retry of the same job: two concurrently claimed
	// jobs of one conversation therefore write to two DIFFERENT transcript
	// files, instead of every background worker sharing runChild's own "w1"
	// fallback. GoalIndex is this goal's position in the ORIGINAL swarm_spawn
	// call -- runWithHeartbeat always calls runChild with a hardcoded index 0,
	// so deliverSuccess/deliverDeadLetter restore the real index from here.
	// FanoutKey is the ONE identity every goal of the SAME swarm_spawn call
	// shares (delegationFanoutKey) -- the grouping key the fan-out nudge sweep
	// (delegation_fanout.go) claims and delivers by.
	ChildID   string `json:"child_id,omitempty"`
	GoalIndex int    `json:"goal_index,omitempty"`
	FanoutKey string `json:"fanout_key,omitempty"`
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
// (internal/cron/deliver.go). *steer.PostgresStore satisfies this by
// construction.
//
// PushDelegationResult (51-11 Task 3) is Push widened in place with the
// fan-out key -- 51-PATTERNS.md's rule for exactly this situation is to
// extend the SAME interface, never declare a second one. Push's own locked
// (conv, source, text string) error signature is unchanged; every existing
// caller of the narrower Push method keeps compiling untouched.
type SteerPublisher interface {
	Push(conv, source, text string) error
	PushDelegationResult(conv, source, text, fanoutKey string) error
}

// DelegationJobStore is the seam over the shared Postgres ingestion-job queue
// (documents.PostgresIngestionJobStore) the enqueue + claim loop need.
// Declared HERE so internal/swarm depends on internal/documents' concrete
// request/row types (reusing the shipped lease/fence/backoff engine, D-01)
// without internal/documents ever importing swarm.
type DelegationJobStore interface {
	Create(ctx context.Context, req documents.CreateIngestionJobRequest) (documents.IngestionJob, error)
	Claim(ctx context.Context, req documents.ClaimIngestionJobsRequest) ([]documents.IngestionJob, error)
	StageDelegationDelivery(ctx context.Context, req documents.StageDelegationDeliveryRequest) (documents.IngestionJob, error)
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

// DelegationEnqueuer, EnqueueDelegation, delegationIdempotencyKey,
// delegationChildID, delegationFanoutKey and the payload codec live in
// delegation_enqueue.go (split out, 51-11, CLAUDE.md's 600-LOC ceiling).

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

	// slots bounds the jobs in flight at once (batchSize); inFlight joins them
	// (Wait). Both belong to the loop, not to a pass: ProcessOnce dispatches and
	// returns, so the runtime ticker keeps claiming while workers run.
	slots     chan struct{}
	slotsOnce sync.Once
	inFlight  sync.WaitGroup
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

// claimBatch claims up to n due job_type=swarm_delegation rows (the query
// filters at claim time -- an ingestion row is never claimed, and never stolen
// from the document ingestion worker's own claim loop) and refuses a misrouted
// row BEFORE anything in the batch runs.
func (l *DelegationClaimLoop) claimBatch(ctx context.Context, n int) ([]documents.IngestionJob, error) {
	if l == nil || l.Store == nil {
		return nil, fmt.Errorf("delegation claim loop has no store")
	}
	if l.IdentityID == "" {
		return nil, fmt.Errorf("delegation claim loop has no identity")
	}
	jobs, err := l.Store.Claim(ctx, documents.ClaimIngestionJobsRequest{
		IdentityID:    l.IdentityID,
		JobType:       JobTypeSwarmDelegation,
		WorkerID:      l.workerID(),
		LeaseDuration: l.leaseDuration(),
		BatchSize:     n,
	})
	if err != nil {
		return nil, err
	}
	for _, job := range jobs {
		if job.IdentityID != l.IdentityID {
			return nil, fmt.Errorf("claimed delegation job %q has unexpected identity %q", job.ID, job.IdentityID)
		}
		if job.JobType != JobTypeSwarmDelegation {
			return nil, fmt.Errorf("claimed delegation job %q has unexpected job_type %q", job.ID, job.JobType)
		}
	}
	return jobs, nil
}

// ProcessOnce is one claim pass: it claims only as many due rows as there are
// free slots (batchSize) and dispatches each to its own goroutine WITHOUT
// waiting, returning the number dispatched. cmd/aura drives it from the shared
// runtime ticker (runtimeTenantIngestionProcessor, asset_processing_worker.go)
// exactly like every other resident processor; Wait joins the dispatched jobs.
// A job's own failure is already recorded on its row by processJob, so here it
// is only logged.
//
// Why dispatch-and-return (the shape before 2026-08-29 claimed a batch and ran
// it to completion inside the pass): every claimed row holds a lease from the
// claim onward, but the heartbeat that renews it only starts inside processJob,
// and a row that arrives after the pass claimed waits until EVERY job of that
// pass finishes. Measured live (live-check/d03/RESULTS.md finding F): two
// delegations issued 1s apart -- the second spawned 125s after the first, once
// the first was reaped; then, with the batch merely concurrent inside the pass,
// a row created 3s after a claim was still queued two minutes later. A 300s
// worker (the AURA_LOOP_MAX_WALLCLOCK_SEC ceiling) ahead of a waiting row lets
// the 300s lease expire in the queue and the row fence out as lease-lost: work
// silently lost, which SWARM-09 forbids.
func (l *DelegationClaimLoop) ProcessOnce(ctx context.Context) (int, error) {
	if l == nil {
		return 0, fmt.Errorf("delegation claim loop has no store")
	}
	l.slotsOnce.Do(func() { l.slots = make(chan struct{}, l.batchSize()) })
	free := cap(l.slots) - len(l.slots)
	if free == 0 {
		return 0, nil
	}
	jobs, err := l.claimBatch(ctx, free)
	if err != nil {
		return 0, err
	}
	for _, job := range jobs {
		l.slots <- struct{}{}
		l.inFlight.Go(func() {
			defer func() { <-l.slots }()
			if err := l.processJob(ctx, job); err != nil {
				slog.Warn("swarm.delegation.job_failed", "job_id", job.ID, "err", err)
			}
		})
	}
	return len(jobs), nil
}

// Wait blocks until every job ProcessOnce dispatched has finished. Tests and
// one-shot callers pair it with ProcessOnce; the daemon never waits (a worker
// cancelled at shutdown records nothing further -- its lease reclaim on the
// next boot is the recovery path, as for any crash).
func (l *DelegationClaimLoop) Wait() {
	l.inFlight.Wait()
}

// Run polls ProcessOnce until ctx is cancelled, then waits for the jobs in
// flight. A pass error is logged and swallowed -- one bad pass never kills the
// loop, mirroring internal/cron's own "log and continue" discipline.
func (l *DelegationClaimLoop) Run(ctx context.Context) error {
	ticker := time.NewTicker(l.pollInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			l.Wait()
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
	pending, err := pendingDeliveryFromJob(job)
	if err != nil {
		return l.recordFailure(ctx, job, err)
	}
	if pending != nil {
		return l.deliverPending(ctx, job, payload, pending)
	}
	// A delivery-only retry is intentionally claimable beyond max_attempts, but an
	// expired worker lease is not a fresh execution budget. Claim increments the
	// count, so a running row reclaimed after its final attempt arrives here at
	// max+1 and must be dead-lettered without constructing another worker.
	if job.MaxAttempts > 0 && job.AttemptCount > job.MaxAttempts {
		return l.recordFailure(ctx, job, errors.New("delegation lease expired after final attempt"))
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

// deliverSuccess persists the terminal report under the active lease before
// projecting it to the conversation and steer queues. Any projection or final
// transition failure schedules a delivery-only retry; processJob detects the
// staged payload on the next claim and never invokes the model again.
func (l *DelegationClaimLoop) deliverSuccess(ctx context.Context, job documents.IngestionJob, payload DelegationPayload, report ChildReport) error {
	// Goal/Attempts/GoalIndex (51-11) travel from the payload/job row, not from
	// runChild's own report -- a worker never sees its own dispatch metadata,
	// only its goal text as a prompt, and runWithHeartbeat always calls
	// runChild with a hardcoded index 0 (the index only feeds the SYNCHRONOUS
	// wave's own per-goal report slot). ChildID needs no override here: once
	// runWithHeartbeat sets rc.ChildID = payload.ChildID, runChild's own
	// report already carries the correct, stable id.
	report.Goal = payload.Goal
	report.GoalIndex = payload.GoalIndex
	report.Attempts = job.AttemptCount
	pending, err := l.stageTerminalDelivery(ctx, job, payload, report, "succeeded", "", "",
		"swarm_delegation.succeeded", "delegation delivered")
	if err != nil {
		return err
	}
	return l.deliverPending(ctx, job, payload, pending)
}

// recordFailure reuses jobs_worker.go's recordHandlerFailure branch verbatim:
// retry with backoff below max_attempts, dead_letter at the cap. No second
// retry/backoff implementation.
func (l *DelegationClaimLoop) recordFailure(ctx context.Context, job documents.IngestionJob, cause error) error {
	message := cause.Error()
	if job.MaxAttempts > 0 && job.AttemptCount >= job.MaxAttempts {
		return l.deliverDeadLetter(ctx, job, message)
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

// deliverDeadLetter is recordFailure's dead-letter tail (51-11, SWARM-12
// leg 1, T-51-55). For a valid payload it stages and projects the terminal
// failure before transitioning the job, so delivery infrastructure failure
// remains retryable even after the worker attempt cap. A malformed payload
// cannot produce an addressed report; that branch transitions directly and
// logs the skipped projection.
//
// ChildID prefers payload.ChildID (51-11's stable, deterministic id, minted
// at EnqueueDelegation time) and falls back to job.ID only when it is empty
// -- a row enqueued before this field existed. recordFailure can fire before
// any worker ever started (a payload decode failure, an identity mismatch),
// so there is no worker-produced child id to prefer instead.
func (l *DelegationClaimLoop) deliverDeadLetter(ctx context.Context, job documents.IngestionJob, message string) error {
	payload, err := delegationPayloadFromJob(job)
	if err != nil {
		slog.Warn("swarm.delegation.dead_letter_record_skipped", "job_id", job.ID, "err", err)
		_, transitionErr := l.Store.UpdateStatus(ctx, documents.TransitionIngestionJobRequest{
			IdentityID: job.IdentityID, JobID: job.ID, WorkerID: l.workerID(),
			LeaseGeneration: job.LeaseGeneration, Status: "dead_letter",
			ErrorCode: "handler_failed", ErrorMessage: message,
			EventType: "swarm_delegation.dead_letter", EventMessage: message,
		})
		return transitionErr
	}
	childID := payload.ChildID
	if childID == "" {
		childID = job.ID
	}
	report := ChildReport{
		ChildID:   childID,
		GoalIndex: payload.GoalIndex,
		Status:    StatusDeadLetter,
		Error:     message,
		Goal:      payload.Goal,
		Attempts:  job.AttemptCount,
	}
	pending, err := l.stageTerminalDelivery(ctx, job, payload, report, "dead_letter", "handler_failed", message,
		"swarm_delegation.dead_letter", message)
	if err != nil {
		return err
	}
	slog.Warn("swarm.delegation.dead_letter", "job_id", job.ID, "attempts", job.AttemptCount, "err", message)
	return l.deliverPending(ctx, job, payload, pending)
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
