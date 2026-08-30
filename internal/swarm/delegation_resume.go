// delegation_resume.go is the SWARM-06/SC#4 half plan 51-06a made possible but did not
// build: a background worker OPENS its own attributed pause and parks its queue row
// (Task 1), and the answer RESUMES that exact worker (Task 2) -- rebuilt through the
// SAME runChild every synchronous swarm invocation uses, seeded with its own persisted
// history plus the operator's answer as the pending ask_user tool result.
//
// D-00 (LibreChat, read before designing): SerializableJobData is deliberately
// reference-free ("no object references, suitable for Redis/external storage") --
// DelegationResumeState mirrors that discipline, plain values only, so it survives a
// daemon restart riding the queue row's EXISTING payload jsonb (D-01: no new table).
// LibreChat's discoveredTools exists because tools found via tool_search before a HITL
// pause are absent from the schema-only toolMap of its rebuilt graph. Aura solves the
// SAME problem structurally: NewLlmAgent re-derives the promoted set from the seeded
// history (internal/agent/llm_agent_construct.go:38-39), so DelegationResumeState
// carries NO derived tool list -- persisting one would be exactly the unanchored path
// deriveActivated's tool_call anchoring exists to forbid.
package swarm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// DelegationResumeState is the reference-free (D-00) continuation snapshot a parked
// delegation job's payload jsonb carries across a worker's pause. Task 1 mints it
// (minus AnswerContent, not yet known) in the SAME transaction that parks the queue row;
// the resume observer (Task 2) fills in AnswerContent from the pause's resumed_answer
// immediately before un-parking, so the claimed row already carries everything the
// rebuild needs with no second read of aura.paused_states.
type DelegationResumeState struct {
	// WorkerID is the ingestion job's own id (D-13's level identity, minted once at
	// park time) -- also the SAME value askuser.InsertParams.OwningWorkerID carries, so
	// a rebuild's sanity check (job.ID == WorkerID) is a single string comparison.
	WorkerID string `json:"worker_id"`
	// Goal/Context/Depth/ConversationID/ParentRunID rebuild the same brief at the same
	// level (mirrors DelegationPayload's own fields -- duplicated here rather than read
	// back off the parent DelegationPayload so DelegationResumeState stays a single
	// self-contained snapshot).
	Goal           string `json:"goal"`
	Context        string `json:"context,omitempty"`
	Depth          int    `json:"depth"`
	ConversationID string `json:"conversation_id"`
	ParentRunID    string `json:"parent_run_id,omitempty"`
	// PendingToolCallID is the ask_user call the answer is the result OF. Without it the
	// answer is an orphan RoleTool message and the model re-asks.
	PendingToolCallID string `json:"pending_tool_call_id"`
	// PendingActionID + PauseToken are 51-06a's fence (D-12): the resume claims exactly
	// this pause, never a stale one from an earlier pause/resume cycle on the same job.
	PendingActionID string `json:"pending_action_id"`
	PauseToken      string `json:"pause_token"`
	// AgentIdentity is the tenant identity that owned the job when it paused
	// (LibreChat's agent_id rule, D-00's second trap): a rebuild under a DIFFERENT
	// identity than the one that paused refuses loudly rather than mis-executing the
	// paused tool calls under someone else's registry/gateway/budget.
	AgentIdentity string `json:"agent_identity"`
	// History is the worker's accumulated []llm.Message, VERBATIM, up to and including
	// the assistant message that issued the ask_user call. Verbatim is load-bearing, not
	// stylistic: it is BOTH the continuation and the tool-permission grant (the
	// tool_search assistant ToolCalls entry paired with its RoleTool result), so no
	// truncation, summarization or reordering may separate that pair.
	History []llm.Message `json:"history"`
	// AnswerContent is empty at park time; Task 2's observer fills it in from the
	// answered pause's resumed_answer immediately before un-parking.
	AnswerContent string `json:"answer_content,omitempty"`
}

// buildResumeTurns is the ENTIRE "how the answer becomes the tool result" mechanism
// (Task 2): the persisted history plus one final RoleTool message answering the exact
// ask_user call that paused. Seeded into RunConfig.ResumeTurns, which runChild threads
// into LlmAgentConfig.UserTurns -- NewLlmAgent's deriveActivated/deriveEverLoaded do the
// rest (they re-derive the promoted set from the seeded history at construction), so
// this is the whole tool-permission story with zero lines changed under internal/agent.
func buildResumeTurns(state *DelegationResumeState) []llm.Message {
	turns := make([]llm.Message, 0, len(state.History)+1)
	turns = append(turns, state.History...)
	turns = append(turns, llm.Message{
		Role:       llm.RoleTool,
		ToolCallID: state.PendingToolCallID,
		Content:    state.AnswerContent,
	})
	return turns
}

// mintFreshUUID mints a fresh UUIDv7 as a string -- the shared idiom Task 1's pause
// token and pending_action_id (D-12) both use, mirroring runner_persist.go's own
// "a fresh token keys the pending" mint site.
func mintFreshUUID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("mint uuid: %w", err)
	}
	return id.String(), nil
}

// PauseAndPark is the ONE-transaction seam Task 1 needs: a claim-loop worker's
// AwaitingInput report must open its own attributed pause AND park its queue row
// atomically -- a pause with no parked row would be answered into nothing; a parked row
// with no pause would never be answered. Declared HERE (the consumer, D-A2-02); a
// pool-backed adapter composing *askuser.Store.InsertTx and
// *documents.PostgresIngestionJobStore.ParkIngestionJobAwaitingInputTx over ONE
// db.WithIdentityTx satisfies it at cmd/aura -- mirrors runner.PoolResumeCommitter's
// own cross-store tx composition (resume_committer.go), never a second concurrency
// story.
type PauseAndPark interface {
	// OpenPauseAndPark writes pause AND park in ONE transaction. parked=false (with a
	// nil error) means the park's conditional UPDATE matched zero rows -- the lease was
	// already lost between the claim and this call -- and the pause was NOT written
	// (the whole transaction rolled back); the caller does nothing further, mirroring
	// runWithHeartbeat's own silent "someone else now owns this row" return.
	OpenPauseAndPark(ctx context.Context, pause askuser.InsertParams, park documents.ParkAwaitingInputRequest) (parked bool, err error)
}

const invalidDelegationResumeCode = "invalid_delegation_resume"

var errInvalidAnsweredDelegation = errors.New("invalid answered delegation")

// RejectAnsweredDelegationRequest describes one invalid answered row that must
// be terminally quarantined under its resumed pause fence.
type RejectAnsweredDelegationRequest struct {
	IdentityID      string
	JobID           string
	PendingActionID string
	ErrorCode       string
	ErrorMessage    string
}

// AnsweredDelegationRejector terminally resolves a structurally invalid answered
// row under its pending-action fence so it cannot be selected again.
type AnsweredDelegationRejector interface {
	RejectAnsweredDelegation(context.Context, RejectAnsweredDelegationRequest) (bool, error)
}

// DelegationResumeObserver watches for delegation pauses that have been answered
// (through the SAME generic /api/approvals -> Runner.SubmitAnswers bridge every other
// pause resolves through -- 51-06a's Source: p.WorkerID() projection is what surfaces a
// worker's pause there) and returns their parked queue row to claimable exactly once.
// It does NOT answer pauses itself and does NOT run the worker -- the shipped
// ClaimIngestionJobs loop claims the un-parked row, and delegation_queue.go's
// processJob detects the resume state and rebuilds through runChild.
type DelegationResumeObserver struct {
	Store    DelegationJobStore
	Rejector AnsweredDelegationRejector
}

// NewDelegationResumeObserver builds an observer bound to store.
func NewDelegationResumeObserver(store DelegationJobStore) *DelegationResumeObserver {
	observer := &DelegationResumeObserver{Store: store}
	if rejector, ok := store.(AnsweredDelegationRejector); ok {
		observer.Rejector = rejector
	}
	return observer
}

// ProcessOnce lists identityID's answered-but-still-parked jobs and un-parks each
// EXACTLY ONCE: RowsAffected==1 for exactly one caller is UnparkIngestionJob's own
// conditional-UPDATE idempotency key, so a second observer pass -- or one racing this
// same pass -- un-parks zero rows for an already-claimed job. Per-row failures are
// aggregated after the batch; structurally invalid rows are terminally rejected.
func (o *DelegationResumeObserver) ProcessOnce(ctx context.Context, identityID string, limit int) (int, error) {
	if o == nil || o.Store == nil {
		return 0, fmt.Errorf("delegation resume observer has no store")
	}
	if identityID == "" {
		return 0, fmt.Errorf("delegation resume observer has no identity")
	}
	jobs, err := o.Store.ListAnsweredAwaitingInput(ctx, identityID, limit)
	if err != nil {
		return 0, fmt.Errorf("delegation resume observer list: %w", err)
	}
	unparked := 0
	var batchErrors []error
	for _, job := range jobs {
		ok, err := o.unparkOne(ctx, job)
		if err != nil {
			if errors.Is(err, errInvalidAnsweredDelegation) {
				if rejectErr := o.rejectInvalid(ctx, job, err); rejectErr != nil {
					batchErrors = append(batchErrors, errors.Join(err, rejectErr))
				} else {
					batchErrors = append(batchErrors, err)
				}
			} else {
				batchErrors = append(batchErrors, err)
			}
			continue
		}
		if ok {
			unparked++
		}
	}
	return unparked, errors.Join(batchErrors...)
}

func (o *DelegationResumeObserver) rejectInvalid(ctx context.Context, job documents.AnsweredAwaitingInputJob, cause error) error {
	if o.Rejector == nil {
		return fmt.Errorf("delegation resume observer job %s: no answered-row rejector configured", job.JobID)
	}
	_, err := o.Rejector.RejectAnsweredDelegation(ctx, RejectAnsweredDelegationRequest{
		IdentityID: job.IdentityID, JobID: job.JobID, PendingActionID: job.PendingActionID,
		ErrorCode: invalidDelegationResumeCode, ErrorMessage: cause.Error(),
	})
	if err != nil {
		return fmt.Errorf("delegation resume observer job %s: reject invalid row: %w", job.JobID, err)
	}
	return nil
}

// unparkOne completes the job's payload with the answer and un-parks it. A payload that
// no longer decodes as a DelegationResumeState (should be structurally impossible --
// Task 1 always writes one before parking) or a pending_action_id that no longer
// matches the fence is a loud error, never a silent skip: either means the pause this
// job is now claiming is NOT the one it thinks it is.
func (o *DelegationResumeObserver) unparkOne(ctx context.Context, job documents.AnsweredAwaitingInputJob) (bool, error) {
	payload, err := delegationPayloadFromMap(job.Payload)
	if err != nil {
		return false, fmt.Errorf("%w: delegation resume observer job %s: %v", errInvalidAnsweredDelegation, job.JobID, err)
	}
	if payload.Resume == nil {
		return false, fmt.Errorf("%w: delegation resume observer job %s: parked with no resume state", errInvalidAnsweredDelegation, job.JobID)
	}
	if payload.Resume.PendingActionID != job.PendingActionID {
		return false, fmt.Errorf("%w: delegation resume observer job %s: fence mismatch (state=%s pause=%s)", errInvalidAnsweredDelegation,
			job.JobID, payload.Resume.PendingActionID, job.PendingActionID)
	}
	var answer askuser.ResumeAnswer
	if err := json.Unmarshal(job.ResumedAnswer, &answer); err != nil {
		return false, fmt.Errorf("%w: delegation resume observer job %s: decode answer: %v", errInvalidAnsweredDelegation, job.JobID, err)
	}
	payload.Resume.AnswerContent = answer.Content
	payloadMap, err := delegationPayloadMap(payload)
	if err != nil {
		return false, fmt.Errorf("%w: delegation resume observer job %s: encode payload: %v", errInvalidAnsweredDelegation, job.JobID, err)
	}
	n, err := o.Store.UnparkIngestionJob(ctx, documents.UnparkIngestionJobRequest{
		IdentityID: job.IdentityID, JobID: job.JobID, Payload: payloadMap,
	})
	if err != nil {
		return false, fmt.Errorf("delegation resume observer job %s: unpark: %w", job.JobID, err)
	}
	return n > 0, nil
}

// delegationPayloadFromMap decodes a raw payload map (already unmarshalled off the
// jsonb column) into a DelegationPayload, without requiring a documents.IngestionJob
// wrapper the way delegationPayloadFromJob does -- the resume observer reads the
// payload straight off AnsweredAwaitingInputJob, which carries no other IngestionJob
// fields.
func delegationPayloadFromMap(m map[string]any) (DelegationPayload, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return DelegationPayload{}, fmt.Errorf("delegation payload encode: %w", err)
	}
	var p DelegationPayload
	if err := json.Unmarshal(b, &p); err != nil {
		return DelegationPayload{}, fmt.Errorf("delegation payload decode: %w", err)
	}
	return p, nil
}
