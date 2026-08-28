package documents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrIngestionJobLeaseLost means a worker no longer owns the job lease and
// must not publish any further state for that attempt.
var ErrIngestionJobLeaseLost = errors.New("ingestion job lease lost")

// IngestionJob is the durable document/asset processing queue row.
type IngestionJob struct {
	ID                 string         `json:"id"`
	IdentityID         string         `json:"identity_id"`
	JobType            string         `json:"job_type"`
	AssetID            string         `json:"asset_id,omitempty"`
	Status             string         `json:"status"`
	IdempotencyKey     string         `json:"idempotency_key"`
	Stage              string         `json:"stage"`
	AttemptCount       int            `json:"attempt_count"`
	MaxAttempts        int            `json:"max_attempts"`
	PipelineGeneration int64          `json:"pipeline_generation"`
	AttemptGeneration  int64          `json:"attempt_generation"`
	LeaseGeneration    int64          `json:"lease_generation"`
	LockedBy           string         `json:"locked_by,omitempty"`
	LockedUntil        time.Time      `json:"locked_until"`
	NextAttemptAt      time.Time      `json:"next_attempt_at"`
	Payload            map[string]any `json:"payload"`
	ErrorCode          string         `json:"error_code,omitempty"`
	ErrorMessage       string         `json:"error_message,omitempty"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
	CompletedAt        time.Time      `json:"completed_at"`
}

// CreateIngestionJobRequest carries a durable job enqueue request.
type CreateIngestionJobRequest struct {
	IdentityID         string
	JobType            string
	AssetID            string
	Status             string
	IdempotencyKey     string
	Stage              string
	MaxAttempts        int
	NextAttemptAt      time.Time
	Payload            map[string]any
	PipelineGeneration int64
}

// ClaimIngestionJobsRequest carries owner-scoped worker lease parameters.
// JobType is an OPTIONAL filter: empty means "claim across every job_type"
// (the document ingestion worker's existing, unchanged behavior); a non-empty
// value scopes the claim to that type (Phase 51's swarm delegation claim loop
// uses this so it never steals a lease from the document ingestion worker,
// and vice versa, even though both claim against the SAME table).
type ClaimIngestionJobsRequest struct {
	IdentityID    string
	JobType       string
	WorkerID      string
	LeaseDuration time.Duration
	BatchSize     int
}

// TransitionIngestionJobRequest carries a fenced terminal state transition.
type TransitionIngestionJobRequest struct {
	IdentityID      string
	JobID           string
	WorkerID        string
	LeaseGeneration int64
	Status          string
	Stage           string
	ErrorCode       string
	ErrorMessage    string
	EventType       string
	EventMessage    string
	EventDetail     map[string]any
	TraceID         string
}

// RetryIngestionJobRequest carries a fenced retry transition.
type RetryIngestionJobRequest struct {
	IdentityID      string
	JobID           string
	WorkerID        string
	LeaseGeneration int64
	Stage           string
	ErrorCode       string
	ErrorMessage    string
	EventMessage    string
	NextAttemptAt   time.Time
}

// HeartbeatIngestionJobRequest carries a fenced lease renewal.
type HeartbeatIngestionJobRequest struct {
	IdentityID      string
	JobID           string
	WorkerID        string
	LeaseGeneration int64
	LeaseDuration   time.Duration
}

// PostgresIngestionJobStore implements the durable ingestion queue with RLS.
type PostgresIngestionJobStore struct {
	pool *pgxpool.Pool
}

// NewPostgresIngestionJobStore builds a Postgres-backed durable ingestion job store.
func NewPostgresIngestionJobStore(pool *pgxpool.Pool) *PostgresIngestionJobStore {
	return &PostgresIngestionJobStore{pool: pool}
}

// Create inserts or returns a durable ingestion job by stable identity-scoped key.
func (s *PostgresIngestionJobStore) Create(ctx context.Context, req CreateIngestionJobRequest) (IngestionJob, error) {
	p, err := createIngestionJobParams(req)
	if err != nil {
		return IngestionJob{}, err
	}
	var row sqlc.AuraIngestionJobs
	err = s.withIdentity(ctx, req.IdentityID, func(q *sqlc.Queries) error {
		var queryErr error
		row, queryErr = q.CreateIngestionJob(ctx, p)
		return queryErr
	})
	if err != nil {
		return IngestionJob{}, err
	}
	return ingestionJobFromSQL(row)
}

// Claim leases queued or expired-running jobs for one owner.
func (s *PostgresIngestionJobStore) Claim(ctx context.Context, req ClaimIngestionJobsRequest) ([]IngestionJob, error) {
	identityID, err := pgUUID("ingestion job identity id", req.IdentityID)
	if err != nil {
		return nil, err
	}
	var rows []sqlc.ClaimIngestionJobsRow
	err = s.withIdentity(ctx, req.IdentityID, func(q *sqlc.Queries) error {
		var queryErr error
		rows, queryErr = q.ClaimIngestionJobs(ctx, sqlc.ClaimIngestionJobsParams{
			IdentityID: identityID, JobType: pgText(req.JobType), LockedBy: pgText(req.WorkerID),
			LeaseDuration: pgInterval(req.LeaseDuration),
			BatchSize:     int32(req.BatchSize), //nolint:gosec // configured worker batch is bounded.
		})
		return queryErr
	})
	if err != nil {
		return nil, err
	}
	out := make([]IngestionJob, 0, len(rows))
	for _, row := range rows {
		job, convertErr := ingestionJobFromSQL(row)
		if convertErr != nil {
			return nil, convertErr
		}
		out = append(out, job)
	}
	return out, nil
}

// UpdateStatus records one fenced lifecycle transition and its event atomically.
func (s *PostgresIngestionJobStore) UpdateStatus(ctx context.Context, req TransitionIngestionJobRequest) (IngestionJob, error) {
	identityID, jobID, detail, err := transitionIngestionJobParams(req)
	if err != nil {
		return IngestionJob{}, err
	}
	var row sqlc.UpdateIngestionJobStatusRow
	err = s.withIdentity(ctx, req.IdentityID, func(q *sqlc.Queries) error {
		var queryErr error
		row, queryErr = q.UpdateIngestionJobStatus(ctx, sqlc.UpdateIngestionJobStatusParams{
			ID: jobID, IdentityID: identityID, LockedBy: pgText(req.WorkerID),
			LeaseGeneration: req.LeaseGeneration, Status: req.Status, Stage: req.Stage,
			ErrorCode: req.ErrorCode, ErrorMessage: req.ErrorMessage,
			EventType: req.EventType, EventMessage: req.EventMessage,
			EventDetail: detail, TraceID: req.TraceID,
		})
		return queryErr
	})
	if err != nil {
		return IngestionJob{}, fencedIngestionJobError("transition", err)
	}
	return ingestionJobFromSQL(row)
}

// Retry returns a claimed job to queued using the active worker fence.
func (s *PostgresIngestionJobStore) Retry(ctx context.Context, req RetryIngestionJobRequest) (IngestionJob, error) {
	identityID, jobID, err := ingestionJobFence(req.IdentityID, req.JobID)
	if err != nil {
		return IngestionJob{}, err
	}
	var row sqlc.RetryIngestionJobRow
	err = s.withIdentity(ctx, req.IdentityID, func(q *sqlc.Queries) error {
		var queryErr error
		row, queryErr = q.RetryIngestionJob(ctx, sqlc.RetryIngestionJobParams{
			ID: jobID, IdentityID: identityID, LockedBy: pgText(req.WorkerID),
			LeaseGeneration: req.LeaseGeneration, Stage: req.Stage,
			ErrorCode: req.ErrorCode, ErrorMessage: req.ErrorMessage,
			NextAttemptAt: pgTime(req.NextAttemptAt), EventMessage: req.EventMessage,
		})
		return queryErr
	})
	if err != nil {
		return IngestionJob{}, fencedIngestionJobError("retry", err)
	}
	return ingestionJobFromSQL(row)
}

// Heartbeat renews a lease only while the owner, worker, and fence still match.
func (s *PostgresIngestionJobStore) Heartbeat(ctx context.Context, req HeartbeatIngestionJobRequest) (IngestionJob, error) {
	identityID, jobID, err := ingestionJobFence(req.IdentityID, req.JobID)
	if err != nil {
		return IngestionJob{}, err
	}
	var row sqlc.AuraIngestionJobs
	err = s.withIdentity(ctx, req.IdentityID, func(q *sqlc.Queries) error {
		var queryErr error
		row, queryErr = q.HeartbeatIngestionJob(ctx, sqlc.HeartbeatIngestionJobParams{
			ID: jobID, IdentityID: identityID, LockedBy: pgText(req.WorkerID),
			LeaseGeneration: req.LeaseGeneration, LeaseDuration: pgInterval(req.LeaseDuration),
		})
		return queryErr
	})
	if err != nil {
		return IngestionJob{}, fencedIngestionJobError("heartbeat", err)
	}
	return ingestionJobFromSQL(row)
}

// CountByStatus returns one owner's durable job count for a status.
func (s *PostgresIngestionJobStore) CountByStatus(ctx context.Context, identityID, status string) (int64, error) {
	pgIdentityID, err := pgUUID("ingestion job identity id", identityID)
	if err != nil {
		return 0, err
	}
	var count int64
	err = s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		var queryErr error
		count, queryErr = q.CountIngestionJobsByStatus(ctx, sqlc.CountIngestionJobsByStatusParams{
			IdentityID: pgIdentityID, Status: status,
		})
		return queryErr
	})
	if err != nil {
		return 0, fmt.Errorf("count ingestion jobs by status: %w", err)
	}
	return count, nil
}

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

// ResolveIngestionJobAwaitingInput transitions ONE parked row straight to 'failed' --
// never dead_letter, which means "retried to exhaustion", a different outcome from a
// human declining to answer. The conditional UPDATE (status must still be
// 'awaiting_input') is the idempotency key: a sweep racing an operator's late answer (or
// a second sweep pass over an already-resolved row) resolves zero rows, so this is safe
// to retry after a partial failure elsewhere in the sweep (e.g. a subsequent
// trace-write failure never leaves this row silently re-resolved).
func (s *PostgresIngestionJobStore) ResolveIngestionJobAwaitingInput(ctx context.Context, req ResolveAwaitingInputRequest) (int64, error) {
	identityID, jobID, err := ingestionJobFence(req.IdentityID, req.JobID)
	if err != nil {
		return 0, err
	}
	var n int64
	err = s.withIdentity(ctx, req.IdentityID, func(q *sqlc.Queries) error {
		var qErr error
		n, qErr = q.ResolveIngestionJobAwaitingInput(ctx, sqlc.ResolveIngestionJobAwaitingInputParams{
			ErrorMessage: req.ErrorMessage, ID: jobID, IdentityID: identityID,
		})
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
	lim := limit
	if lim <= 0 {
		lim = 100
	}
	var rows []sqlc.ListAnsweredAwaitingInputJobsRow
	err = s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		var qErr error
		rows, qErr = q.ListAnsweredAwaitingInputJobs(ctx, sqlc.ListAnsweredAwaitingInputJobsParams{
			IdentityID: pgIdentityID, RowLimit: int32(lim), //nolint:gosec // an operator-configured sweep batch size, always small.
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

func (s *PostgresIngestionJobStore) withIdentity(ctx context.Context, identityID string, fn func(*sqlc.Queries) error) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("ingestion job store is not configured")
	}
	if identityID == "" {
		return fmt.Errorf("ingestion job identity id is required")
	}
	return db.WithIdentityTx(ctx, s.pool, identityID, fn)
}

func createIngestionJobParams(req CreateIngestionJobRequest) (sqlc.CreateIngestionJobParams, error) {
	identityID, err := pgUUID("ingestion job identity id", req.IdentityID)
	if err != nil {
		return sqlc.CreateIngestionJobParams{}, err
	}
	assetID, err := optionalUUIDFromString("asset id", req.AssetID)
	if err != nil {
		return sqlc.CreateIngestionJobParams{}, err
	}
	payload, err := ingestionJobPayloadJSON(req.Payload)
	if err != nil {
		return sqlc.CreateIngestionJobParams{}, err
	}
	return sqlc.CreateIngestionJobParams{
		IdentityID: identityID, JobType: req.JobType, AssetID: assetID,
		Status:         req.Status,
		IdempotencyKey: req.IdempotencyKey, Stage: req.Stage,
		MaxAttempts:   int32(req.MaxAttempts), //nolint:gosec // caller controls a small retry count.
		NextAttemptAt: pgTime(req.NextAttemptAt), Payload: payload,
		PipelineGeneration: req.PipelineGeneration,
	}, nil
}

func transitionIngestionJobParams(req TransitionIngestionJobRequest) (pgtype.UUID, pgtype.UUID, []byte, error) {
	identityID, jobID, err := ingestionJobFence(req.IdentityID, req.JobID)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, nil, err
	}
	detail, err := ingestionEventDetailJSON(req.EventDetail)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, nil, err
	}
	return identityID, jobID, detail, nil
}

func ingestionJobFence(identityID, jobID string) (pgtype.UUID, pgtype.UUID, error) {
	pgIdentityID, err := pgUUID("ingestion job identity id", identityID)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, err
	}
	pgJobID, err := pgUUID("ingestion job id", jobID)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, err
	}
	return pgIdentityID, pgJobID, nil
}

func fencedIngestionJobError(action string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s ingestion job: %w", action, ErrIngestionJobLeaseLost)
	}
	return err
}

// ingestionJobRow is the set of row structs sqlc emits for the ingestion job queries.
// Every one of those queries returns the full aura.ingestion_jobs column list in table
// order, so the four generated structs are field-identical to the model and each converts
// straight into it — which is what lets a single mapper serve all of them. A query that
// projects a narrower or reordered column list stops compiling here rather than silently
// growing a fifth copy of this mapping.
type ingestionJobRow interface {
	sqlc.AuraIngestionJobs | sqlc.ClaimIngestionJobsRow | sqlc.UpdateIngestionJobStatusRow |
		sqlc.RetryIngestionJobRow
}

func ingestionJobFromSQL[R ingestionJobRow](row R) (IngestionJob, error) {
	rec := sqlc.AuraIngestionJobs(row)
	payload, err := ingestionJobPayloadFromJSON(rec.Payload)
	if err != nil {
		return IngestionJob{}, err
	}
	return IngestionJob{
		ID: uuidString(rec.ID), IdentityID: uuidString(rec.IdentityID), JobType: rec.JobType,
		AssetID: uuidString(rec.AssetID), Status: rec.Status,
		IdempotencyKey: rec.IdempotencyKey, Stage: rec.Stage,
		AttemptCount: int(rec.AttemptCount), MaxAttempts: int(rec.MaxAttempts),
		PipelineGeneration: rec.PipelineGeneration, AttemptGeneration: rec.AttemptGeneration,
		LeaseGeneration: rec.LeaseGeneration, LockedBy: textString(rec.LockedBy),
		LockedUntil: timeValue(rec.LockedUntil), NextAttemptAt: timeValue(rec.NextAttemptAt),
		Payload: payload, ErrorCode: rec.ErrorCode, ErrorMessage: rec.ErrorMessage,
		CreatedAt: timeValue(rec.CreatedAt), UpdatedAt: timeValue(rec.UpdatedAt),
		CompletedAt: timeValue(rec.CompletedAt),
	}, nil
}

func ingestionJobPayloadJSON(payload map[string]any) ([]byte, error) {
	if payload == nil {
		payload = map[string]any{}
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("ingestion job payload: %w", err)
	}
	return out, nil
}

// ingestionEventDetailJSON encodes the detail blob the transition statement writes into
// aura.ingestion_events. NOT NULL with a jsonb default, so a nil map must travel as `{}`.
func ingestionEventDetailJSON(detail map[string]any) ([]byte, error) {
	if detail == nil {
		detail = map[string]any{}
	}
	out, err := json.Marshal(detail)
	if err != nil {
		return nil, fmt.Errorf("ingestion event detail: %w", err)
	}
	return out, nil
}

func ingestionJobPayloadFromJSON(data []byte) (map[string]any, error) {
	if len(data) == 0 {
		return map[string]any{}, nil
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("ingestion job payload: %w", err)
	}
	if payload == nil {
		return map[string]any{}, nil
	}
	return payload, nil
}

func optionalUUIDFromString(field, value string) (pgtype.UUID, error) {
	if value == "" {
		return pgtype.UUID{}, nil
	}
	u, err := pgUUID(field, value)
	if err != nil {
		return pgtype.UUID{}, err
	}
	return u, nil
}

func pgTime(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		t = time.Now().UTC()
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func pgInterval(d time.Duration) pgtype.Interval {
	if d <= 0 {
		d = time.Minute
	}
	return pgtype.Interval{Microseconds: d.Microseconds(), Valid: true}
}
