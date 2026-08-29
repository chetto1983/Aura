// delegation_run.go is DelegationClaimLoop's "run one claimed job through the SAME
// runChild every synchronous worker uses" half -- split out of delegation_queue.go
// (CLAUDE.md's file-size ceiling) as its own concern: minting the trusted root
// operation context + heartbeat goroutine a claimed job's worker needs
// (runWithHeartbeat/delegationOperationContext), and 51-06b Task 1's
// "AwaitingInput opens its own attributed pause and parks its row" path
// (openPauseAndPark/pauseOptionsJSON). Queue lifecycle (claim/retry/succeed/
// dead_letter) stays in delegation_queue.go.
package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/llm"
)

// openPauseAndPark mints the pause's fence (D-12) and level identity (D-13), persists
// the full DelegationResumeState into the job's payload, and writes the pause AND the
// park in ONE transaction via l.PauseParker -- a pause with no parked row would be
// answered into nothing; a parked row with no pause would never be answered. On success
// it surfaces the question through the SAME plan-51-10 delivery seam the consolidated
// report uses (record to the origin conversation AND push where reachable, 268580e23)
// -- no new delivery policy. Explicitly does NOT auto-deny (D-13: hermes'
// _subagent_auto_deny is the fallback only where no durable queue exists, and this
// phase has one) and does NOT reuse the agent_job path (its maxAutoRejects=8 is the
// exact inverse of SWARM-06).
func (l *DelegationClaimLoop) openPauseAndPark(ctx context.Context, job documents.IngestionJob, payload DelegationPayload, report ChildReport, history []llm.Message) error {
	if l.PauseParker == nil {
		return fmt.Errorf("delegation claim loop has no pause-and-park writer configured")
	}
	pendingActionID, err := mintFreshUUID()
	if err != nil {
		return fmt.Errorf("delegation open pause: %w", err)
	}
	pauseToken, err := mintFreshUUID()
	if err != nil {
		return fmt.Errorf("delegation open pause: %w", err)
	}

	resumed := payload
	resumed.Resume = &DelegationResumeState{
		WorkerID:          job.ID,
		Goal:              payload.Goal,
		Context:           payload.Context,
		Depth:             payload.Depth,
		ConversationID:    payload.ConversationID,
		ParentRunID:       payload.ParentRunID,
		PendingToolCallID: report.ToolCallID,
		PendingActionID:   pendingActionID,
		PauseToken:        pauseToken,
		AgentIdentity:     job.IdentityID,
		History:           history,
	}
	payloadMap, err := delegationPayloadMap(resumed)
	if err != nil {
		return fmt.Errorf("delegation open pause payload: %w", err)
	}

	options, err := pauseOptionsJSON(report.Options)
	if err != nil {
		return fmt.Errorf("delegation open pause options: %w", err)
	}
	ownerID := job.ID
	fenceID := pendingActionID
	insert := askuser.InsertParams{
		Token:          pauseToken,
		ConversationID: payload.ConversationID,
		// tools.KindClarification (internal/agent/tools/ask_user.go) is the ONLY
		// definition of this vocabulary -- askuser.InsertParams.Kind is a plain
		// string with no local constants of its own, and aura.paused_states'
		// own CHECK constraint enumerates the same three values.
		Kind:            tools.KindClarification,
		Question:        report.Question,
		Options:         options,
		ToolCallID:      report.ToolCallID,
		OwningWorkerID:  &ownerID,
		PendingActionID: &fenceID,
	}
	park := documents.ParkAwaitingInputRequest{
		IdentityID:      job.IdentityID,
		JobID:           job.ID,
		WorkerID:        l.workerID(),
		LeaseGeneration: job.LeaseGeneration,
		Payload:         payloadMap,
	}

	parked, err := l.PauseParker.OpenPauseAndPark(ctx, insert, park)
	if err != nil {
		return fmt.Errorf("delegation open pause: %w", err)
	}
	if !parked {
		// Lease already lost between the claim and this call -- another worker now
		// owns this row. Mirrors runWithHeartbeat's own silent return: writing
		// anything further here would race that worker's own transition.
		return nil
	}

	if l.Delivery != nil {
		questionText := fmt.Sprintf("Worker asks: %s", report.Question)
		// ctx carries the job's identity (bound once in processJob) for the recorder's RLS write.
		if _, derr := l.Delivery.Deliver(ctx, payload, questionText); derr != nil {
			return fmt.Errorf("delegation question deliver: %w", derr)
		}
	}
	return nil
}

// pauseOptionsJSON encodes a worker's ask_user option labels for askuser.InsertParams.
// Options: a bare JSON string decodes on the read side as {Label, Value} both set to
// that string (tools.Option.UnmarshalJSON's documented contract), so passing the flat
// []string labels straight through is the correct wire shape. An empty/nil slice stays
// nil (SQL NULL), never the literal JSON `null`.
func pauseOptionsJSON(labels []string) (json.RawMessage, error) {
	if len(labels) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(labels)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

// runWithHeartbeat runs the claimed job through the SAME runChild every
// synchronous swarm invocation uses -- no second worker construction, no
// second event loop, so Pitfall 2's fix (791dcd7e0, gateway.WithDelegatedDispatch)
// rides along automatically -- while a sibling goroutine renews the Postgres
// lease on a fixed interval, copying jobs_worker.go's
// handleWithHeartbeat/maintainLease shape (goroutine racing the handler,
// context.WithCancel, buffered result channel).
//
// The lease heartbeat here and D-03's own inactivity tick (child_staleness.go,
// plan 51-09) are deliberately TWO independent bounds, not one: the heartbeat
// keeps the QUEUE ROW's lease alive on a fixed interval regardless of worker
// activity (so a lease reclaim never races a live worker), while
// child_staleness.go -- ticked from inside runChild's own event loop, the ONE
// place a worker's liveness is judged for every caller -- reaps the WORKER
// itself on genuine silence. config.GuardSwarmStaleness enforces at boot that
// the inactivity deadline (AURA_SWARM_CHILD_IDLE_SEC) stays strictly shorter
// than this lease (AURA_SWARM_DELEGATION_LEASE_SEC), so the two bounds can
// never cross.
func (l *DelegationClaimLoop) runWithHeartbeat(ctx context.Context, job documents.IngestionJob, payload DelegationPayload) (ChildReport, []llm.Message, error) {
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
	// ChildID (51-11): payload.ChildID is EnqueueDelegation's deterministic,
	// stable per-goal id -- runChild prefers it over its own "w1" fallback, so
	// two concurrently claimed jobs of one conversation write to two DIFFERENT
	// transcript files instead of interleaving into the same one.
	rc.ChildID = payload.ChildID
	if payload.Resume != nil {
		// 51-06b Task 2: the ENTIRE tool-permission story. Seeding UserTurns (via
		// ResumeTurns, below runChild) with the persisted history re-derives the
		// promoted deferred-tool set for free -- NewLlmAgent's deriveActivated/
		// deriveEverLoaded read it straight off the seeded history at construction.
		// Nothing is re-applied from a list, nothing is rediscovered.
		rc.ResumeTurns = buildResumeTurns(payload.Resume)
	}

	budget, err := agent.NewBudgetFromEnv()
	if err != nil {
		cancel()
		<-heartbeatErr
		return ChildReport{}, nil, fmt.Errorf("delegation job budget: %w", err)
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
		return ChildReport{}, nil, fmt.Errorf("delegation operation context: %w", err)
	}

	report, history := runChild(delegationCtx, rc, budget, 0, payload.Goal)
	cancel()
	if hbErr := <-heartbeatErr; hbErr != nil {
		return ChildReport{}, nil, hbErr
	}
	return report, history, nil
}

// delegationOperationContext mints the trusted root operation context a
// claimed delegation job's worker needs before it can dispatch ANY mutating
// tool (gateway.beginOperation denies "operation context missing" otherwise --
// see runWithHeartbeat's doc comment). ctx must already carry the job's
// identity -- processJob binds it once at the claim-loop boundary, and the
// worker's own identity-scoped tools (document_search, skill_manage,
// send_file_ingest, a NESTED swarm_spawn) resolve it from there. Mirrors
// cron/dispatch.go's scheduledOperationContext, one layer further out (a
// worker instead of a scheduled task).
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
	operationCtx, err := idempotency.WithOperation(ctx, operation)
	if err != nil {
		return nil, fmt.Errorf("delegation operation: %w", err)
	}
	return operationCtx, nil
}
