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

// Kept local because swarm already imports documents; importing swarm here would cycle.
const delegationJobType = "swarm_delegation"

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
	jobs, err := s.CreateBatch(ctx, []CreateIngestionJobRequest{req})
	if err != nil {
		return IngestionJob{}, err
	}
	return jobs[0], nil
}

// CreateBatch inserts or returns every job in one identity-scoped transaction.
func (s *PostgresIngestionJobStore) CreateBatch(ctx context.Context, reqs []CreateIngestionJobRequest) ([]IngestionJob, error) {
	if len(reqs) == 0 {
		return []IngestionJob{}, nil
	}
	identityID := reqs[0].IdentityID
	params := make([]sqlc.CreateIngestionJobParams, len(reqs))
	for i, req := range reqs {
		if req.IdentityID != identityID {
			return nil, fmt.Errorf("create ingestion job batch: request %d identity does not match batch identity", i)
		}
		p, err := createIngestionJobParams(req)
		if err != nil {
			return nil, fmt.Errorf("create ingestion job batch request %d: %w", i, err)
		}
		params[i] = p
	}

	jobs := make([]IngestionJob, 0, len(params))
	err := s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		for i, p := range params {
			row, queryErr := q.CreateIngestionJob(ctx, p)
			if queryErr != nil {
				return fmt.Errorf("create ingestion job batch request %d: %w", i, queryErr)
			}
			job, convertErr := ingestionJobFromSQL(row)
			if convertErr != nil {
				return fmt.Errorf("decode ingestion job batch request %d: %w", i, convertErr)
			}
			jobs = append(jobs, job)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return jobs, nil
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

// CountUnfinishedDelegationJobs returns how many job_type=swarm_delegation rows of
// identityID still carry fanoutKey in their payload and sit in queued, running or
// awaiting_input (51-11 Task 3, the fan-out nudge sweep's eligibility check). A fanoutKey
// nobody wrote, or one whose every job already reached a terminal status, returns 0.
func (s *PostgresIngestionJobStore) CountUnfinishedDelegationJobs(ctx context.Context, identityID, fanoutKey string) (int, error) {
	pgIdentityID, err := pgUUID("ingestion job identity id", identityID)
	if err != nil {
		return 0, err
	}
	var count int64
	err = s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		var queryErr error
		count, queryErr = q.CountUnfinishedDelegationJobsForFanout(ctx, sqlc.CountUnfinishedDelegationJobsForFanoutParams{
			IdentityID: pgIdentityID, FanoutKey: fanoutKey,
		})
		return queryErr
	})
	if err != nil {
		return 0, fmt.Errorf("count unfinished delegation jobs for fanout: %w", err)
	}
	return int(count), nil
}

// DelegationJobRow is the durable worker state exposed to swarm_status.
type DelegationJobRow struct {
	ID           string
	Goal         string
	ChildID      string
	Status       string
	AttemptCount int
	MaxAttempts  int
	CreatedAt    time.Time
	CompletedAt  time.Time
	ErrorMessage string
}

// ListDelegationJobs returns the newest workers for one identity and conversation.
func (s *PostgresIngestionJobStore) ListDelegationJobs(ctx context.Context, identityID, conversationID string, limit int) ([]DelegationJobRow, error) {
	return s.listDelegationJobs(ctx, identityID, conversationID, pgtype.Text{}, limit)
}

// FindDelegationJob resolves a child without applying the bounded list window.
func (s *PostgresIngestionJobStore) FindDelegationJob(ctx context.Context, identityID, conversationID, childID string) (DelegationJobRow, bool, error) {
	rows, err := s.listDelegationJobs(ctx, identityID, conversationID, pgtype.Text{String: childID, Valid: true}, 1)
	if err != nil {
		return DelegationJobRow{}, false, err
	}
	if len(rows) == 0 {
		return DelegationJobRow{}, false, nil
	}
	return rows[0], true, nil
}

func (s *PostgresIngestionJobStore) listDelegationJobs(ctx context.Context, identityID, conversationID string, childID pgtype.Text, limit int) ([]DelegationJobRow, error) {
	if limit <= 0 || int64(limit) > int64(1<<31-1) {
		return nil, fmt.Errorf("delegation job limit must be between 1 and %d", int64(1<<31-1))
	}
	pgIdentityID, err := pgUUID("ingestion job identity id", identityID)
	if err != nil {
		return nil, err
	}
	var rows []sqlc.ListDelegationJobsForConversationRow
	err = s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		var queryErr error
		rows, queryErr = q.ListDelegationJobsForConversation(ctx, sqlc.ListDelegationJobsForConversationParams{
			IdentityID: pgIdentityID, JobType: delegationJobType, ConversationID: conversationID,
			ChildID: childID, RowLimit: int32(limit),
		})
		return queryErr
	})
	if err != nil {
		return nil, fmt.Errorf("list delegation jobs: %w", err)
	}
	out := make([]DelegationJobRow, 0, len(rows))
	for _, r := range rows {
		row, decodeErr := delegationJobRowFromSQL(r)
		if decodeErr != nil {
			return nil, decodeErr
		}
		out = append(out, row)
	}
	return out, nil
}

func delegationJobRowFromSQL(r sqlc.ListDelegationJobsForConversationRow) (DelegationJobRow, error) {
	payload, err := ingestionJobPayloadFromJSON(r.Payload)
	if err != nil {
		return DelegationJobRow{}, fmt.Errorf("decode delegation job %s payload: %w", uuidString(r.ID), err)
	}
	goal, goalOK := payload["goal"].(string)
	childID, childIDOK := payload["child_id"].(string)
	if !goalOK || goal == "" || !childIDOK || childID == "" {
		return DelegationJobRow{}, fmt.Errorf("decode delegation job %s payload: goal and child_id are required", uuidString(r.ID))
	}
	return DelegationJobRow{
		ID: uuidString(r.ID), Goal: goal, ChildID: childID, Status: r.Status,
		AttemptCount: int(r.AttemptCount), MaxAttempts: int(r.MaxAttempts),
		CreatedAt: timeValue(r.CreatedAt), CompletedAt: timeValue(r.CompletedAt),
		ErrorMessage: r.ErrorMessage,
	}, nil
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
