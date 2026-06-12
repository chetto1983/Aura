package documents

import (
	"context"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type JobStore interface {
	Create(ctx context.Context, params CreateJobParams) (Job, error)
	Get(ctx context.Context, id string) (Job, error)
	GetByDocumentID(ctx context.Context, documentID string) (Job, error)
	UpdateStatus(ctx context.Context, id string, status JobStatus, message string) (Job, error)
	UpdateProgress(ctx context.Context, id string, status JobStatus, sparseChunks, embeddedChunks int) (Job, error)
	ListRecent(ctx context.Context, limit int) ([]Job, error)
}

type PostgresJobStore struct {
	q *sqlc.Queries
}

func NewPostgresJobStore(pool *pgxpool.Pool) *PostgresJobStore {
	return &PostgresJobStore{q: sqlc.New(pool)}
}

func (s *PostgresJobStore) Create(ctx context.Context, params CreateJobParams) (Job, error) {
	row, err := s.q.CreateDocumentIngestJob(ctx, sqlc.CreateDocumentIngestJobParams{
		SourceID:     params.SourceID,
		SourceKind:   params.SourceKind,
		DocumentID:   params.DocumentID,
		ContentHash:  params.ContentHash,
		OriginalPath: params.OriginalPath,
		FileName:     params.FileName,
		MimeType:     params.MIMEType,
		SizeBytes:    params.SizeBytes,
		Status:       string(params.Status),
	})
	if err != nil {
		return Job{}, err
	}
	return jobFromSQL(row), nil
}

func (s *PostgresJobStore) Get(ctx context.Context, id string) (Job, error) {
	pgID, err := pgUUID("job id", id)
	if err != nil {
		return Job{}, err
	}
	row, err := s.q.GetDocumentIngestJob(ctx, pgID)
	if err != nil {
		return Job{}, err
	}
	return jobFromSQL(row), nil
}

func (s *PostgresJobStore) GetByDocumentID(ctx context.Context, documentID string) (Job, error) {
	row, err := s.q.GetDocumentIngestJobByDocumentID(ctx, documentID)
	if err != nil {
		return Job{}, err
	}
	return jobFromSQL(row), nil
}

func (s *PostgresJobStore) UpdateStatus(ctx context.Context, id string, status JobStatus, message string) (Job, error) {
	pgID, err := pgUUID("job id", id)
	if err != nil {
		return Job{}, err
	}
	row, err := s.q.UpdateDocumentIngestJobStatus(ctx, sqlc.UpdateDocumentIngestJobStatusParams{
		ID:     pgID,
		Status: string(status),
		Error:  pgText(message),
	})
	if err != nil {
		return Job{}, err
	}
	return jobFromSQL(row), nil
}

func (s *PostgresJobStore) UpdateProgress(ctx context.Context, id string, status JobStatus, sparseChunks, embeddedChunks int) (Job, error) {
	pgID, err := pgUUID("job id", id)
	if err != nil {
		return Job{}, err
	}
	row, err := s.q.UpdateDocumentIngestJobProgress(ctx, sqlc.UpdateDocumentIngestJobProgressParams{
		ID:             pgID,
		Status:         string(status),
		SparseChunks:   int32(sparseChunks),   //nolint:gosec // chunk counts are bounded by in-memory slice length.
		EmbeddedChunks: int32(embeddedChunks), //nolint:gosec // chunk counts are bounded by in-memory slice length.
	})
	if err != nil {
		return Job{}, err
	}
	return jobFromSQL(row), nil
}

func (s *PostgresJobStore) ListRecent(ctx context.Context, limit int) ([]Job, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.q.ListRecentDocumentIngestJobs(ctx, int32(limit)) //nolint:gosec // CLI/service caps callers to small positive limits.
	if err != nil {
		return nil, err
	}
	out := make([]Job, 0, len(rows))
	for _, row := range rows {
		out = append(out, jobFromSQL(row))
	}
	return out, nil
}

func jobFromSQL(row sqlc.AuraDocumentIngestJobs) Job {
	return Job{
		ID:             uuidString(row.ID),
		SourceID:       row.SourceID,
		SourceKind:     row.SourceKind,
		DocumentID:     row.DocumentID,
		ContentHash:    row.ContentHash,
		OriginalPath:   row.OriginalPath,
		FileName:       row.FileName,
		MIMEType:       row.MimeType,
		SizeBytes:      row.SizeBytes,
		Status:         JobStatus(row.Status),
		SparseChunks:   int(row.SparseChunks),
		EmbeddedChunks: int(row.EmbeddedChunks),
		Error:          textString(row.Error),
		CreatedAt:      timeValue(row.CreatedAt),
		UpdatedAt:      timeValue(row.UpdatedAt),
		SearchableAt:   timeValue(row.SearchableAt),
		CompletedAt:    timeValue(row.CompletedAt),
	}
}

func pgUUID(field, value string) (pgtype.UUID, error) {
	u, err := uuid.Parse(value)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("invalid %s %q: %w", field, value, err)
	}
	return pgtype.UUID{Bytes: u, Valid: true}, nil
}

func uuidString(value pgtype.UUID) string {
	if !value.Valid {
		return ""
	}
	return uuid.UUID(value.Bytes).String()
}

func pgText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}

func textString(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func timeValue(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time
}
