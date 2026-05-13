package toolindex

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// SQLiteStateStore persists the tool_index_state table created by
// migration 5. The schema is intentionally minimal — Reconciler does the
// diff in memory, this layer just stores key→row rows.
type SQLiteStateStore struct {
	db *sql.DB
}

// NewSQLiteStateStore wraps an open *sql.DB. The caller owns the
// connection lifecycle; this constructor does not run migrations
// (migrations are run once at boot in cmd/aura/main.go).
func NewSQLiteStateStore(db *sql.DB) *SQLiteStateStore {
	return &SQLiteStateStore{db: db}
}

// LoadAll returns every row indexed by tool_name. An empty database
// returns a non-nil empty map so callers can range over it safely.
func (s *SQLiteStateStore) LoadAll(ctx context.Context) (map[string]StateRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT tool_name, content_hash, point_id, embed_model, indexed_at FROM tool_index_state`)
	if err != nil {
		return nil, fmt.Errorf("toolindex state: query: %w", err)
	}
	defer rows.Close()

	out := make(map[string]StateRow)
	for rows.Next() {
		var r StateRow
		if err := rows.Scan(&r.ToolName, &r.ContentHash, &r.PointID, &r.EmbedModel, &r.IndexedAt); err != nil {
			return nil, fmt.Errorf("toolindex state: scan: %w", err)
		}
		out[r.ToolName] = r
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("toolindex state: rows: %w", err)
	}
	return out, nil
}

// Upsert inserts-or-replaces a row by tool_name. Returns an error on
// constraint or transport failure; the caller is expected to have already
// applied the corresponding Qdrant upsert.
func (s *SQLiteStateStore) Upsert(ctx context.Context, row StateRow) error {
	if row.ToolName == "" {
		return errors.New("toolindex state: tool_name required")
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tool_index_state (tool_name, content_hash, point_id, embed_model, indexed_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(tool_name) DO UPDATE SET
		   content_hash = excluded.content_hash,
		   point_id     = excluded.point_id,
		   embed_model  = excluded.embed_model,
		   indexed_at   = excluded.indexed_at`,
		row.ToolName, row.ContentHash, row.PointID, row.EmbedModel, row.IndexedAt)
	if err != nil {
		return fmt.Errorf("toolindex state: upsert %s: %w", row.ToolName, err)
	}
	return nil
}

// Remove deletes a row. Idempotent — missing rows are not an error.
func (s *SQLiteStateStore) Remove(ctx context.Context, toolName string) error {
	if toolName == "" {
		return errors.New("toolindex state: tool_name required")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM tool_index_state WHERE tool_name = ?`, toolName)
	if err != nil {
		return fmt.Errorf("toolindex state: remove %s: %w", toolName, err)
	}
	return nil
}
