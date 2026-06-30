package documents

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

// IngestionJob is the durable document/asset processing queue row.
type IngestionJob struct {
	ID             string         `json:"id"`
	JobType        string         `json:"job_type"`
	DocumentID     string         `json:"document_id,omitempty"`
	VersionID      string         `json:"version_id,omitempty"`
	Status         string         `json:"status"`
	IdempotencyKey string         `json:"idempotency_key"`
	Stage          string         `json:"stage"`
	AttemptCount   int            `json:"attempt_count"`
	MaxAttempts    int            `json:"max_attempts"`
	LockedBy       string         `json:"locked_by,omitempty"`
	LockedUntil    time.Time      `json:"locked_until,omitempty"`
	NextAttemptAt  time.Time      `json:"next_attempt_at,omitempty"`
	Payload        map[string]any `json:"payload"`
	ErrorCode      string         `json:"error_code,omitempty"`
	ErrorMessage   string         `json:"error_message,omitempty"`
	CreatedAt      time.Time      `json:"created_at,omitempty"`
	UpdatedAt      time.Time      `json:"updated_at,omitempty"`
	CompletedAt    time.Time      `json:"completed_at,omitempty"`
}

// CreateIngestionJobRequest carries a durable job enqueue request.
type CreateIngestionJobRequest struct {
	JobType        string
	DocumentID     string
	VersionID      string
	Status         string
	IdempotencyKey string
	Stage          string
	MaxAttempts    int
	NextAttemptAt  time.Time
	Payload        map[string]any
}

// ClaimIngestionJobsRequest carries worker lease parameters for queued jobs.
type ClaimIngestionJobsRequest struct {
	WorkerID      string
	LeaseDuration time.Duration
	BatchSize     int
}

// PostgresIngestionJobStore implements the durable ingestion job queue with sqlc.
type PostgresIngestionJobStore struct {
	q *sqlc.Queries
}

// NewPostgresIngestionJobStore builds a Postgres-backed durable ingestion job store.
func NewPostgresIngestionJobStore(db sqlc.DBTX) *PostgresIngestionJobStore {
	return &PostgresIngestionJobStore{q: sqlc.New(db)}
}

// Create inserts or returns a durable ingestion job by idempotency key.
func (s *PostgresIngestionJobStore) Create(ctx context.Context, req CreateIngestionJobRequest) (IngestionJob, error) {
	payload, err := ingestionJobPayloadJSON(req.Payload)
	if err != nil {
		return IngestionJob{}, err
	}
	documentID, err := optionalUUIDFromString("document id", req.DocumentID)
	if err != nil {
		return IngestionJob{}, err
	}
	versionID, err := optionalUUIDFromString("version id", req.VersionID)
	if err != nil {
		return IngestionJob{}, err
	}
	row, err := s.q.CreateIngestionJob(ctx, sqlc.CreateIngestionJobParams{
		JobType:        req.JobType,
		DocumentID:     documentID,
		VersionID:      versionID,
		Status:         req.Status,
		IdempotencyKey: req.IdempotencyKey,
		Stage:          req.Stage,
		MaxAttempts:    int32(req.MaxAttempts), //nolint:gosec // caller controls a small retry count.
		NextAttemptAt:  pgTime(req.NextAttemptAt),
		Payload:        payload,
	})
	if err != nil {
		return IngestionJob{}, err
	}
	return ingestionJobFromSQL(row)
}

// Claim leases queued jobs using the generated SKIP LOCKED query.
func (s *PostgresIngestionJobStore) Claim(ctx context.Context, req ClaimIngestionJobsRequest) ([]IngestionJob, error) {
	rows, err := s.q.ClaimIngestionJobs(ctx, sqlc.ClaimIngestionJobsParams{
		LockedBy:      pgtype.Text{String: req.WorkerID, Valid: req.WorkerID != ""},
		LeaseDuration: pgInterval(req.LeaseDuration),
		BatchSize:     int32(req.BatchSize), //nolint:gosec // worker batch sizes are small positive ints.
	})
	if err != nil {
		return nil, err
	}
	out := make([]IngestionJob, 0, len(rows))
	for _, row := range rows {
		job, err := ingestionJobFromSQL(row)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, nil
}

// UpdateStatus records one durable job lifecycle transition.
func (s *PostgresIngestionJobStore) UpdateStatus(ctx context.Context, id, status, stage, code, message string) (IngestionJob, error) {
	pgID, err := pgUUID("ingestion job id", id)
	if err != nil {
		return IngestionJob{}, err
	}
	row, err := s.q.UpdateIngestionJobStatus(ctx, sqlc.UpdateIngestionJobStatusParams{
		ID:           pgID,
		Status:       status,
		Stage:        stage,
		ErrorCode:    code,
		ErrorMessage: message,
	})
	if err != nil {
		return IngestionJob{}, err
	}
	return ingestionJobFromSQL(row)
}

// Retry returns a claimed job to the queued state for a later attempt.
func (s *PostgresIngestionJobStore) Retry(ctx context.Context, id, stage, code, message string, nextAttemptAt time.Time) (IngestionJob, error) {
	pgID, err := pgUUID("ingestion job id", id)
	if err != nil {
		return IngestionJob{}, err
	}
	row, err := s.q.RetryIngestionJob(ctx, sqlc.RetryIngestionJobParams{
		ID:            pgID,
		Stage:         stage,
		ErrorCode:     code,
		ErrorMessage:  message,
		NextAttemptAt: pgTime(nextAttemptAt),
	})
	if err != nil {
		return IngestionJob{}, err
	}
	return ingestionJobFromSQL(row)
}

func ingestionJobFromSQL(row sqlc.AuraIngestionJobs) (IngestionJob, error) {
	payload, err := ingestionJobPayloadFromJSON(row.Payload)
	if err != nil {
		return IngestionJob{}, err
	}
	return IngestionJob{
		ID:             uuidString(row.ID),
		JobType:        row.JobType,
		DocumentID:     uuidString(row.DocumentID),
		VersionID:      uuidString(row.VersionID),
		Status:         row.Status,
		IdempotencyKey: row.IdempotencyKey,
		Stage:          row.Stage,
		AttemptCount:   int(row.AttemptCount),
		MaxAttempts:    int(row.MaxAttempts),
		LockedBy:       textString(row.LockedBy),
		LockedUntil:    timeValue(row.LockedUntil),
		NextAttemptAt:  timeValue(row.NextAttemptAt),
		Payload:        payload,
		ErrorCode:      row.ErrorCode,
		ErrorMessage:   row.ErrorMessage,
		CreatedAt:      timeValue(row.CreatedAt),
		UpdatedAt:      timeValue(row.UpdatedAt),
		CompletedAt:    timeValue(row.CompletedAt),
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
