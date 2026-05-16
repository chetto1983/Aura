package freshness

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrVersionConflict is returned by UpdateWithVersion when a concurrent writer
// updated the row between the SELECT and the UPDATE (optimistic concurrency).
var ErrVersionConflict = errors.New("freshness: version conflict")

// Row mirrors the projection_state table schema.
type Row struct {
	ProjectionID       string
	Kind               string
	EmbeddingModelID   string
	EmbeddingDim       int
	IndexBuildID       string
	SchemaVersion      int
	LastFullRebuildAt  int64
	LastIncrementalAt  *int64
	PendingCount       int
	CompletedCount     int
	FailedCount        int
	Status             string
	HealthReason       string
	Version            int
	UpdatedAt          int64
}

// Store provides CRUD access to the projection_state table.
type Store struct {
	db *sql.DB
}

// NewStore creates a Store wrapping the given pool.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// Get returns the row for projectionID. Returns (Row{}, false, nil) when not found.
func (s *Store) Get(ctx context.Context, projectionID string) (Row, bool, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT projection_id, kind, embedding_model_id, embedding_dim,
       index_build_id, schema_version, last_full_rebuild_at, last_incremental_at,
       pending_count, completed_count, failed_count, status, health_reason, version, updated_at
FROM projection_state
WHERE projection_id = ?`, projectionID)
	r, err := scanRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Row{}, false, nil
	}
	if err != nil {
		return Row{}, false, fmt.Errorf("freshness: get %q: %w", projectionID, err)
	}
	return r, true, nil
}

// Upsert inserts or fully replaces the row for row.ProjectionID.
// Sets UpdatedAt to the current Unix timestamp on every call.
func (s *Store) Upsert(ctx context.Context, row Row) (Row, error) {
	row.UpdatedAt = time.Now().Unix()
	_, err := s.db.ExecContext(ctx, `
INSERT INTO projection_state (
    projection_id, kind, embedding_model_id, embedding_dim,
    index_build_id, schema_version, last_full_rebuild_at, last_incremental_at,
    pending_count, completed_count, failed_count, status, health_reason, version, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(projection_id) DO UPDATE SET
    kind                 = excluded.kind,
    embedding_model_id   = excluded.embedding_model_id,
    embedding_dim        = excluded.embedding_dim,
    index_build_id       = excluded.index_build_id,
    schema_version       = excluded.schema_version,
    last_full_rebuild_at = excluded.last_full_rebuild_at,
    last_incremental_at  = excluded.last_incremental_at,
    pending_count        = excluded.pending_count,
    completed_count      = excluded.completed_count,
    failed_count         = excluded.failed_count,
    status               = excluded.status,
    health_reason        = excluded.health_reason,
    version              = excluded.version,
    updated_at           = excluded.updated_at`,
		row.ProjectionID, row.Kind, row.EmbeddingModelID, row.EmbeddingDim,
		row.IndexBuildID, row.SchemaVersion, row.LastFullRebuildAt, row.LastIncrementalAt,
		row.PendingCount, row.CompletedCount, row.FailedCount, row.Status, row.HealthReason,
		row.Version, row.UpdatedAt,
	)
	if err != nil {
		return Row{}, fmt.Errorf("freshness: upsert %q: %w", row.ProjectionID, err)
	}
	return row, nil
}

// UpdateWithVersion applies mutator to the current row then writes back using
// optimistic concurrency: UPDATE ... WHERE projection_id=? AND version=<old>.
// Returns ErrVersionConflict when a concurrent writer changed the row first.
// The version field is incremented automatically after calling mutator.
func (s *Store) UpdateWithVersion(ctx context.Context, projectionID string, mutator func(*Row) error) (Row, error) {
	current, found, err := s.Get(ctx, projectionID)
	if err != nil {
		return Row{}, err
	}
	if !found {
		return Row{}, fmt.Errorf("freshness: update %q: row not found", projectionID)
	}
	oldVersion := current.Version
	if err := mutator(&current); err != nil {
		return Row{}, fmt.Errorf("freshness: update mutator: %w", err)
	}
	current.Version = oldVersion + 1
	current.UpdatedAt = time.Now().Unix()

	res, err := s.db.ExecContext(ctx, `
UPDATE projection_state SET
    kind                 = ?,
    embedding_model_id   = ?,
    embedding_dim        = ?,
    index_build_id       = ?,
    schema_version       = ?,
    last_full_rebuild_at = ?,
    last_incremental_at  = ?,
    pending_count        = ?,
    completed_count      = ?,
    failed_count         = ?,
    status               = ?,
    health_reason        = ?,
    version              = ?,
    updated_at           = ?
WHERE projection_id = ? AND version = ?`,
		current.Kind, current.EmbeddingModelID, current.EmbeddingDim,
		current.IndexBuildID, current.SchemaVersion, current.LastFullRebuildAt,
		current.LastIncrementalAt, current.PendingCount, current.CompletedCount,
		current.FailedCount, current.Status, current.HealthReason,
		current.Version, current.UpdatedAt,
		projectionID, oldVersion,
	)
	if err != nil {
		return Row{}, fmt.Errorf("freshness: update %q: %w", projectionID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Row{}, fmt.Errorf("freshness: update rows affected: %w", err)
	}
	if n == 0 {
		return Row{}, ErrVersionConflict
	}
	return current, nil
}

// BumpPending increments pending_count by delta and refreshes updated_at.
func (s *Store) BumpPending(ctx context.Context, projectionID string, delta int) error {
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx,
		`UPDATE projection_state SET pending_count = pending_count + ?, updated_at = ? WHERE projection_id = ?`,
		delta, now, projectionID,
	)
	if err != nil {
		return fmt.Errorf("freshness: bump pending %q: %w", projectionID, err)
	}
	return nil
}

// MarkRebuildComplete resets pending_count to 0, bumps completed_count, updates
// index_build_id and embedding_model_id, and records last_full_rebuild_at.
// Called from memoryindex.Rebuild at completion.
func (s *Store) MarkRebuildComplete(ctx context.Context, projectionID, newBuildID, newModelID string) error {
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx, `
UPDATE projection_state SET
    pending_count        = 0,
    completed_count      = completed_count + 1,
    index_build_id       = ?,
    embedding_model_id   = ?,
    last_full_rebuild_at = ?,
    updated_at           = ?,
    version              = version + 1
WHERE projection_id = ?`,
		newBuildID, newModelID, now, now, projectionID,
	)
	if err != nil {
		return fmt.Errorf("freshness: mark rebuild complete %q: %w", projectionID, err)
	}
	return nil
}

// List returns all projection_state rows ordered by projection_id.
func (s *Store) List(ctx context.Context) ([]Row, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT projection_id, kind, embedding_model_id, embedding_dim,
       index_build_id, schema_version, last_full_rebuild_at, last_incremental_at,
       pending_count, completed_count, failed_count, status, health_reason, version, updated_at
FROM projection_state
ORDER BY projection_id`)
	if err != nil {
		return nil, fmt.Errorf("freshness: list: %w", err)
	}
	defer rows.Close()

	var result []Row
	for rows.Next() {
		r, err := scanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("freshness: list scan: %w", err)
		}
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("freshness: list rows: %w", err)
	}
	return result, nil
}

// scanner abstracts *sql.Row and *sql.Rows so scanRow works for both.
type scanner interface {
	Scan(dest ...any) error
}

func scanRow(s scanner) (Row, error) {
	var r Row
	err := s.Scan(
		&r.ProjectionID, &r.Kind, &r.EmbeddingModelID, &r.EmbeddingDim,
		&r.IndexBuildID, &r.SchemaVersion, &r.LastFullRebuildAt, &r.LastIncrementalAt,
		&r.PendingCount, &r.CompletedCount, &r.FailedCount, &r.Status, &r.HealthReason,
		&r.Version, &r.UpdatedAt,
	)
	return r, err
}
