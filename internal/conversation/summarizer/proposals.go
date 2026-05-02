package summarizer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrProposalNotFound is returned when a proposed_update row doesn't exist.
var ErrProposalNotFound = errors.New("summarizer: proposal not found")

// ErrProposalConflict is returned when trying to approve/reject an already-decided proposal.
var ErrProposalConflict = errors.New("summarizer: proposal already decided")

// ProposedUpdate is one row from proposed_updates.
type ProposedUpdate struct {
	ID            int64     `json:"id"`
	ChatID        int64     `json:"chat_id"`
	Fact          string    `json:"fact"`
	Action        string    `json:"action"`
	TargetSlug    string    `json:"target_slug,omitempty"`
	Similarity    float64   `json:"similarity"`
	SourceTurnIDs []int64   `json:"source_turn_ids"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
}

// SummariesStore provides CRUD over the proposed_updates table.
type SummariesStore struct {
	db *sql.DB
}

// NewSummariesStore wraps an existing *sql.DB. The migration must already
// have been applied (scheduler.OpenStore handles this).
func NewSummariesStore(db *sql.DB) *SummariesStore {
	return &SummariesStore{db: db}
}

// List returns proposed updates, optionally filtered by status (empty = all).
// Ordered by created_at DESC; capped by limit (0 = default 100).
func (s *SummariesStore) List(ctx context.Context, status string, limit int) ([]ProposedUpdate, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows *sql.Rows
	var err error
	if status != "" {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, chat_id, fact, action, target_slug, similarity, source_turn_ids, status, created_at
			 FROM proposed_updates WHERE status = ? ORDER BY created_at DESC LIMIT ?`,
			status, limit)
	} else {
		rows, err = s.db.QueryContext(ctx,
			`SELECT id, chat_id, fact, action, target_slug, similarity, source_turn_ids, status, created_at
			 FROM proposed_updates ORDER BY created_at DESC LIMIT ?`,
			limit)
	}
	if err != nil {
		return nil, fmt.Errorf("summaries list: %w", err)
	}
	defer rows.Close()

	out := []ProposedUpdate{}
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Get returns a single proposal by ID, or ErrProposalNotFound.
func (s *SummariesStore) Get(ctx context.Context, id int64) (ProposedUpdate, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, chat_id, fact, action, target_slug, similarity, source_turn_ids, status, created_at
		 FROM proposed_updates WHERE id = ?`, id)
	p, err := scanProposal(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ProposedUpdate{}, ErrProposalNotFound
		}
		return ProposedUpdate{}, fmt.Errorf("summaries get: %w", err)
	}
	return p, nil
}

// SetStatus flips the status of a proposal. Returns ErrProposalNotFound if
// no row exists, ErrProposalConflict if already approved or rejected.
func (s *SummariesStore) SetStatus(ctx context.Context, id int64, newStatus string) (ProposedUpdate, error) {
	p, err := s.Get(ctx, id)
	if err != nil {
		return ProposedUpdate{}, err
	}
	if p.Status != "pending" {
		return ProposedUpdate{}, ErrProposalConflict
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE proposed_updates SET status = ? WHERE id = ?`, newStatus, id); err != nil {
		return ProposedUpdate{}, fmt.Errorf("summaries set status: %w", err)
	}
	p.Status = newStatus
	return p, nil
}

type proposalScanner interface {
	Scan(dest ...any) error
}

func scanProposal(r proposalScanner) (ProposedUpdate, error) {
	var p ProposedUpdate
	var idsJSON string
	var createdAt string
	if err := r.Scan(
		&p.ID, &p.ChatID, &p.Fact, &p.Action, &p.TargetSlug,
		&p.Similarity, &idsJSON, &p.Status, &createdAt,
	); err != nil {
		return ProposedUpdate{}, err
	}
	if idsJSON != "" && idsJSON != "null" {
		_ = json.Unmarshal([]byte(idsJSON), &p.SourceTurnIDs)
	}
	if p.SourceTurnIDs == nil {
		p.SourceTurnIDs = []int64{}
	}
	ts, err := time.Parse("2006-01-02 15:04:05", createdAt)
	if err != nil {
		ts, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			return ProposedUpdate{}, fmt.Errorf("parse created_at %q: %w", createdAt, err)
		}
	}
	p.CreatedAt = ts.UTC()
	return p, nil
}
