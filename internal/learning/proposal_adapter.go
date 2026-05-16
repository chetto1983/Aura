package learning

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// SQLProposalStore implements ProposalStore on top of the proposed_updates
// SQLite table. Migration v14 adds the kind and signature_hash columns required
// by this adapter.
type SQLProposalStore struct {
	db *sql.DB
}

// NewSQLProposalStore wraps an existing *sql.DB.
func NewSQLProposalStore(db *sql.DB) *SQLProposalStore {
	return &SQLProposalStore{db: db}
}

// CreateOperationalMemoryProposal inserts a pending operational_memory proposal.
// Returns created=false without error if a row with the same signature_hash
// already exists (any status).
func (s *SQLProposalStore) CreateOperationalMemoryProposal(ctx context.Context, params CreateProposalParams) (bool, error) {
	var existing int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM proposed_updates WHERE signature_hash = ?`, params.SignatureHash).Scan(&existing)
	if err != nil {
		return false, fmt.Errorf("proposal adapter: check existing: %w", err)
	}
	if existing > 0 {
		return false, nil
	}
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO proposed_updates
		  (chat_id, fact, action, target_slug, similarity,
		   source_turn_ids, category, related_slugs, provenance_json,
		   status, kind, signature_hash, created_at)
		VALUES (0, ?, 'new', ?, 1.0, '[]', ?, '[]', '{}', 'pending', 'operational_memory', ?, ?)`,
		params.Summary, params.ToolName, params.ErrorClass, params.SignatureHash, now,
	)
	if err != nil {
		return false, fmt.Errorf("proposal adapter: insert: %w", err)
	}
	return true, nil
}
