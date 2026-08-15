package documents

import (
	"context"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// JobStore persists document ingestion job state.
type JobStore interface {
	Create(ctx context.Context, params CreateJobParams) (Job, error)
	Get(ctx context.Context, id string) (Job, error)
	UpdateStatus(ctx context.Context, id string, status JobStatus, message string) (Job, error)
	ListRecent(ctx context.Context, limit int) ([]Job, error)
}

// PostgresJobStore implements JobStore with sqlc-generated Postgres queries.
type PostgresJobStore struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}

// NewPostgresJobStore builds a Postgres-backed document ingestion job store.
func NewPostgresJobStore(pool *pgxpool.Pool) *PostgresJobStore {
	return &PostgresJobStore{pool: pool, q: sqlc.New(pool)}
}

// Create inserts a new document ingestion job.
func (s *PostgresJobStore) Create(ctx context.Context, params CreateJobParams) (Job, error) {
	identityID, err := pgUUID("job identity id", params.IdentityID)
	if err != nil {
		return Job{}, err
	}
	assetID, err := optionalUUIDFromString("job asset id", params.AssetID)
	if err != nil {
		return Job{}, err
	}
	catalogDocumentID, err := optionalUUIDFromString("job catalog document id", params.CatalogDocumentID)
	if err != nil {
		return Job{}, err
	}
	versionID, err := optionalUUIDFromString("job version id", params.VersionID)
	if err != nil {
		return Job{}, err
	}
	var row sqlc.AuraDocumentIngestJobs
	err = s.withIdentity(ctx, params.IdentityID, func(q *sqlc.Queries) error {
		var queryErr error
		row, queryErr = q.CreateDocumentIngestJob(ctx, sqlc.CreateDocumentIngestJobParams{
			IdentityID: identityID, SourceID: params.SourceID,
			SourceKind:   params.SourceKind,
			DocumentID:   params.DocumentID,
			ContentHash:  params.ContentHash,
			OriginalPath: params.OriginalPath,
			FileName:     params.FileName,
			MimeType:     params.MIMEType,
			SizeBytes:    params.SizeBytes,
			Status:       string(params.Status),
			AssetID:      assetID, CatalogDocumentID: catalogDocumentID, VersionID: versionID,
			PipelineGeneration: params.PipelineGeneration,
		})
		return queryErr
	})
	if err != nil {
		return Job{}, err
	}
	return jobFromSQL(row), nil
}

// Get returns a document ingestion job by id.
func (s *PostgresJobStore) Get(ctx context.Context, id string) (Job, error) {
	pgID, err := pgUUID("job id", id)
	if err != nil {
		return Job{}, err
	}
	identityID, pgIdentityID, err := callerJobIdentity(ctx)
	if err != nil {
		return Job{}, err
	}
	var row sqlc.AuraDocumentIngestJobs
	err = s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		var queryErr error
		row, queryErr = q.GetDocumentIngestJob(ctx, sqlc.GetDocumentIngestJobParams{ID: pgID, IdentityID: pgIdentityID})
		return queryErr
	})
	if err != nil {
		return Job{}, err
	}
	return jobFromSQL(row), nil
}

// UpdateStatus updates a job lifecycle status and optional error message.
func (s *PostgresJobStore) UpdateStatus(ctx context.Context, id string, status JobStatus, message string) (Job, error) {
	pgID, err := pgUUID("job id", id)
	if err != nil {
		return Job{}, err
	}
	identityID, pgIdentityID, err := callerJobIdentity(ctx)
	if err != nil {
		return Job{}, err
	}
	var row sqlc.AuraDocumentIngestJobs
	err = s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		var queryErr error
		row, queryErr = q.UpdateDocumentIngestJobStatus(ctx, sqlc.UpdateDocumentIngestJobStatusParams{
			ID: pgID, IdentityID: pgIdentityID,
			Status: string(status),
			Error:  pgText(message),
		})
		return queryErr
	})
	if err != nil {
		return Job{}, err
	}
	return jobFromSQL(row), nil
}

// ListRecent returns recent document ingestion jobs, newest first.
func (s *PostgresJobStore) ListRecent(ctx context.Context, limit int) ([]Job, error) {
	if limit <= 0 {
		limit = 20
	}
	identityID, pgIdentityID, err := callerJobIdentity(ctx)
	if err != nil {
		return nil, err
	}
	var rows []sqlc.AuraDocumentIngestJobs
	err = s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		var queryErr error
		rows, queryErr = q.ListRecentDocumentIngestJobs(ctx, sqlc.ListRecentDocumentIngestJobsParams{
			IdentityID: pgIdentityID,
			RowLimit:   int32(limit), //nolint:gosec // CLI/service caps callers to small positive limits.
		})
		return queryErr
	})
	if err != nil {
		return nil, err
	}
	out := make([]Job, 0, len(rows))
	for _, row := range rows {
		out = append(out, jobFromSQL(row))
	}
	return out, nil
}

func (s *PostgresJobStore) withIdentity(ctx context.Context, identityID string, fn func(*sqlc.Queries) error) error {
	if s.pool != nil {
		return db.WithIdentityTx(ctx, s.pool, identityID, fn)
	}
	if s.q == nil {
		return fmt.Errorf("document ingest job store is not configured")
	}
	return fn(s.q)
}

func callerJobIdentity(ctx context.Context) (string, pgtype.UUID, error) {
	identityID := identityctx.IdentityID(ctx)
	pgIdentityID, err := pgUUID("job identity id", identityID)
	return identityID, pgIdentityID, err
}

func jobFromSQL(row sqlc.AuraDocumentIngestJobs) Job {
	return Job{
		ID:         uuidString(row.ID),
		IdentityID: uuidString(row.IdentityID),
		AssetID:    uuidString(row.AssetID), CatalogDocumentID: uuidString(row.CatalogDocumentID),
		VersionID: uuidString(row.VersionID), PipelineGeneration: row.PipelineGeneration,
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
