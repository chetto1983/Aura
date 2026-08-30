// jobs_store_awaiting_input.go is PostgresIngestionJobStore's 'awaiting_input' half
// (51-06b, SWARM-06/SC#4): a claimed job parks on a human answer instead of finishing,
// is un-parked exactly once when that answer lands, and is resolved to a terminal
// state when the answer never comes. Split from jobs_store.go (the generic
// create/claim/transition lifecycle, at the 600-LOC ceiling) as its own concern; the
// four statements here are plain conditional UPDATEs / reads over migration 0107's
// widened status vocabulary, each RowsAffected==1-for-one-caller as its own
// idempotency key (the MarkPausedStateResumed idiom).
package documents

import (
	"context"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

// ParkAwaitingInputRequest carries the conditional-update args for parking a claimed
// job at status='awaiting_input' (D-12/D-13 background worker pause, 51-06b Task 1).
// LockedBy/LeaseGeneration must match the CURRENT claim exactly -- the same fencing
// every other transition on this table already applies (Claim/UpdateStatus/Retry/
// Heartbeat) -- so a stale claim (its lease already reclaimed) parks nothing.
type ParkAwaitingInputRequest struct {
	IdentityID      string
	JobID           string
	WorkerID        string
	LeaseGeneration int64
	Payload         map[string]any
}

// ParkIngestionJobAwaitingInputTx parks ONE claimed job as awaiting_input using the
// caller-supplied Queries (bound to the caller's transaction -- it opens NO transaction
// of its own). RowsAffected==0 means the lease was already lost between the claim and
// this call; the caller MUST NOT also write a pause in that case (a pause with no parked
// row would be answered into nothing -- Task 1's own atomicity requirement). This is the
// Tx-bound half a cross-package "pause + park in one transaction" composer (built at
// cmd/aura, mirroring runner.PoolResumeCommitter's cross-store tx composition) calls
// alongside askuser.Store.InsertTx.
func (s *PostgresIngestionJobStore) ParkIngestionJobAwaitingInputTx(ctx context.Context, q *sqlc.Queries, req ParkAwaitingInputRequest) (int64, error) {
	identityID, jobID, err := ingestionJobFence(req.IdentityID, req.JobID)
	if err != nil {
		return 0, err
	}
	payload, err := ingestionJobPayloadJSON(req.Payload)
	if err != nil {
		return 0, fmt.Errorf("park ingestion job payload: %w", err)
	}
	return q.ParkIngestionJobAwaitingInput(ctx, sqlc.ParkIngestionJobAwaitingInputParams{
		Payload: payload, ID: jobID, IdentityID: identityID,
		LockedBy: pgText(req.WorkerID), LeaseGeneration: req.LeaseGeneration,
	})
}

// ParkIngestionJobAwaitingInput parks ONE claimed job as awaiting_input, in a
// transaction scoped to identityID. Thin wrapper over the Tx variant for a caller with
// no cross-store atomicity requirement of its own (e.g. a test fixture).
func (s *PostgresIngestionJobStore) ParkIngestionJobAwaitingInput(ctx context.Context, req ParkAwaitingInputRequest) (int64, error) {
	var n int64
	err := s.withIdentity(ctx, req.IdentityID, func(q *sqlc.Queries) error {
		var qErr error
		n, qErr = s.ParkIngestionJobAwaitingInputTx(ctx, q, req)
		return qErr
	})
	if err != nil {
		return 0, fmt.Errorf("park ingestion job awaiting input: %w", err)
	}
	return n, nil
}

// UnparkIngestionJobRequest carries the args for returning an answered pause's parked
// row to claimable (51-06b Task 2). Payload is the CALLER's already-merged
// DelegationResumeState (the original payload plus the operator's answer content) --
// UnparkIngestionJob replaces the stored payload wholesale so the row the shipped
// ClaimIngestionJobs loop next claims already carries everything the resume rebuild
// needs.
type UnparkIngestionJobRequest struct {
	IdentityID string
	JobID      string
	Payload    map[string]any
}

// UnparkIngestionJob returns ONE answered pause's parked row to status='queued'. The
// conditional UPDATE (status must still be 'awaiting_input') is the idempotency key:
// RowsAffected==1 for exactly one caller, 0 for a second observer pass or a racing one.
// attempt_count is untouched -- an answered question is not a retry.
func (s *PostgresIngestionJobStore) UnparkIngestionJob(ctx context.Context, req UnparkIngestionJobRequest) (int64, error) {
	identityID, jobID, err := ingestionJobFence(req.IdentityID, req.JobID)
	if err != nil {
		return 0, err
	}
	payload, err := ingestionJobPayloadJSON(req.Payload)
	if err != nil {
		return 0, fmt.Errorf("unpark ingestion job payload: %w", err)
	}
	var n int64
	err = s.withIdentity(ctx, req.IdentityID, func(q *sqlc.Queries) error {
		var qErr error
		n, qErr = q.UnparkIngestionJob(ctx, sqlc.UnparkIngestionJobParams{
			Payload: payload, ID: jobID, IdentityID: identityID,
		})
		return qErr
	})
	if err != nil {
		return 0, fmt.Errorf("unpark ingestion job: %w", err)
	}
	return n, nil
}

// ResolveAwaitingInputRequest carries the args for expiring an unanswered worker pause's
// parked row to its terminal state (D-08 extended to the queue row, 51-06b Task 3).
type ResolveAwaitingInputRequest struct {
	IdentityID   string
	JobID        string
	ErrorMessage string
}

// ResolveIngestionJobAwaitingInputTx transitions ONE parked row straight to 'failed'
// using the caller's Queries (bound to the caller's transaction -- it opens NO
// transaction of its own, so the resolution commits or rolls back together with the
// pause expiry and its trace: D-08 applied to the queue row). Never dead_letter, which
// means "retried to exhaustion" -- a different outcome from a human declining to
// answer. The conditional UPDATE (status must still be 'awaiting_input') is the
// idempotency key: a sweep racing an operator's late answer, or a second pass over an
// already-resolved row, resolves zero rows.
func (s *PostgresIngestionJobStore) ResolveIngestionJobAwaitingInputTx(ctx context.Context, q *sqlc.Queries, req ResolveAwaitingInputRequest) (int64, error) {
	identityID, jobID, err := ingestionJobFence(req.IdentityID, req.JobID)
	if err != nil {
		return 0, err
	}
	return q.ResolveIngestionJobAwaitingInput(ctx, sqlc.ResolveIngestionJobAwaitingInputParams{
		ErrorMessage: req.ErrorMessage, ID: jobID, IdentityID: identityID,
	})
}

// ResolveIngestionJobAwaitingInput is the Tx variant in a transaction scoped to
// identityID, for a caller with no cross-store atomicity requirement of its own.
func (s *PostgresIngestionJobStore) ResolveIngestionJobAwaitingInput(ctx context.Context, req ResolveAwaitingInputRequest) (int64, error) {
	var n int64
	err := s.withIdentity(ctx, req.IdentityID, func(q *sqlc.Queries) error {
		var qErr error
		n, qErr = s.ResolveIngestionJobAwaitingInputTx(ctx, q, req)
		return qErr
	})
	if err != nil {
		return 0, fmt.Errorf("resolve ingestion job awaiting input: %w", err)
	}
	return n, nil
}

// AnsweredAwaitingInputJob is one parked job whose pause has been answered (the resume
// observer's read, 51-06b Task 2).
type AnsweredAwaitingInputJob struct {
	JobID           string
	IdentityID      string
	Payload         map[string]any
	PauseToken      string
	PendingActionID string
	ResumedAnswer   []byte // raw {action,content} jsonb -- the caller decodes as askuser.ResumeAnswer
}

// RejectAnsweredDelegationRequest fences terminal quarantine of an invalid
// answered delegation against its identity, job, and resumed pause action.
type RejectAnsweredDelegationRequest struct {
	IdentityID      string
	JobID           string
	PendingActionID string
	ErrorCode       string
	ErrorMessage    string
}

// RejectAnsweredDelegation terminally quarantines an invalid answered job once.
func (s *PostgresIngestionJobStore) RejectAnsweredDelegation(ctx context.Context, req RejectAnsweredDelegationRequest) (bool, error) {
	identityID, jobID, err := ingestionJobFence(req.IdentityID, req.JobID)
	if err != nil {
		return false, err
	}
	var rejected bool
	err = s.withIdentity(ctx, req.IdentityID, func(q *sqlc.Queries) error {
		var queryErr error
		rejected, queryErr = q.RejectAnsweredDelegation(ctx, sqlc.RejectAnsweredDelegationParams{
			ID: jobID, IdentityID: identityID, PendingActionID: req.PendingActionID,
			ErrorCode: req.ErrorCode, ErrorMessage: req.ErrorMessage,
		})
		return queryErr
	})
	if err != nil {
		return false, fmt.Errorf("reject answered delegation: %w", err)
	}
	return rejected, nil
}

// ListAnsweredAwaitingInput lists parked jobs for identityID whose pause has been
// answered (resumed_at IS NOT NULL), joined via the pause token this specific park
// cycle minted (job.payload.resume.pause_token) -- disambiguating a job's CURRENT pause
// from an older, already-resolved one across repeated pause/resume cycles on the same
// job. limit<=0 falls back to 100.
func (s *PostgresIngestionJobStore) ListAnsweredAwaitingInput(ctx context.Context, identityID string, limit int) ([]AnsweredAwaitingInputJob, error) {
	pgIdentityID, err := pgUUID("ingestion job identity id", identityID)
	if err != nil {
		return nil, err
	}
	var rows []sqlc.ListAnsweredAwaitingInputJobsRow
	err = s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		var qErr error
		rows, qErr = q.ListAnsweredAwaitingInputJobs(ctx, sqlc.ListAnsweredAwaitingInputJobsParams{
			IdentityID: pgIdentityID, RowLimit: awaitingInputRowLimit(limit),
		})
		return qErr
	})
	if err != nil {
		return nil, fmt.Errorf("list answered awaiting input jobs: %w", err)
	}
	out := make([]AnsweredAwaitingInputJob, 0, len(rows))
	for _, r := range rows {
		payload, pErr := ingestionJobPayloadFromJSON(r.Payload)
		if pErr != nil {
			return nil, pErr
		}
		out = append(out, AnsweredAwaitingInputJob{
			JobID: uuidString(r.ID), IdentityID: uuidString(r.IdentityID), Payload: payload,
			PauseToken: uuidString(r.PauseToken), PendingActionID: textString(r.PendingActionID),
			ResumedAnswer: r.ResumedAnswer,
		})
	}
	return out, nil
}

// ExpiredAwaitingInputJob is one parked job whose pause the operator never answered
// within the TTL (the per-worker pause sweep's read, 51-06b Task 3). It carries what
// the sweep needs to expire the pause (PauseToken + PendingActionID, the D-12 fence),
// write its trace (ConversationID, Question) and resolve the row (JobID, IdentityID).
type ExpiredAwaitingInputJob struct {
	JobID           string
	IdentityID      string
	PauseToken      string
	PendingActionID string
	ToolCallID      string
	Kind            string
	Question        string
	ConversationID  string
}

// ListExpiredAwaitingInput lists parked jobs for identityID whose pause is still
// unanswered and was created at or before cutoff, oldest first, joined via the pause
// token this park cycle minted (the same join as ListAnsweredAwaitingInput). limit<=0
// falls back to 100.
func (s *PostgresIngestionJobStore) ListExpiredAwaitingInput(ctx context.Context, identityID string, cutoff time.Time, limit int) ([]ExpiredAwaitingInputJob, error) {
	pgIdentityID, err := pgUUID("ingestion job identity id", identityID)
	if err != nil {
		return nil, err
	}
	var rows []sqlc.ListExpiredAwaitingInputJobsRow
	err = s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		var qErr error
		rows, qErr = q.ListExpiredAwaitingInputJobs(ctx, sqlc.ListExpiredAwaitingInputJobsParams{
			IdentityID: pgIdentityID,
			Cutoff:     pgtype.Timestamptz{Time: cutoff, Valid: true},
			RowLimit:   awaitingInputRowLimit(limit),
		})
		return qErr
	})
	if err != nil {
		return nil, fmt.Errorf("list expired awaiting input jobs: %w", err)
	}
	out := make([]ExpiredAwaitingInputJob, 0, len(rows))
	for _, r := range rows {
		out = append(out, ExpiredAwaitingInputJob{
			JobID: uuidString(r.ID), IdentityID: uuidString(r.IdentityID),
			PauseToken: uuidString(r.PauseToken), PendingActionID: textString(r.PendingActionID),
			ToolCallID: r.ToolCallID, Kind: r.Kind, Question: r.Question, ConversationID: uuidString(r.ConversationID),
		})
	}
	return out, nil
}

// awaitingInputRowLimit bounds one observer/sweep pass: limit<=0 falls back to 100.
func awaitingInputRowLimit(limit int) int32 {
	if limit <= 0 {
		limit = 100
	}
	return int32(limit) //nolint:gosec // an operator-configured sweep batch size, always small.
}
