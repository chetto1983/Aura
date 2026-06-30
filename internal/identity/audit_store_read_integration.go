//go:build db_integration

package identity

import (
	"context"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// IdentityAuditStore wraps the generated audit queries for integration tests.
type IdentityAuditStore struct {
	q *sqlc.Queries
}

// NewIdentityAuditStore builds an IdentityAuditStore over an open pool.
func NewIdentityAuditStore(pool *pgxpool.Pool) *IdentityAuditStore {
	return &IdentityAuditStore{q: sqlc.New(pool)}
}

// IdentityAuditRow is the domain projection of one aura.identity_audit row.
type IdentityAuditRow struct {
	ID                  string
	CreatedAt           time.Time
	ActorIdentityID     string
	NewIdentityID       string
	NewIdentityName     string
	GrantedCapabilities []string
	AuthulaUserID       string
}

// IdentityAuditFilter narrows the audit list.
type IdentityAuditFilter struct {
	NewIdentityID string
	Limit         int
}

// ListIdentityAudit returns audit rows newest-first.
func (s *IdentityAuditStore) ListIdentityAudit(ctx context.Context, f IdentityAuditFilter) ([]IdentityAuditRow, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	var newID pgtype.UUID
	if f.NewIdentityID != "" {
		u, err := uuid.Parse(f.NewIdentityID)
		if err != nil {
			return nil, fmt.Errorf("list identity audit: invalid new_identity_id %q: %w", f.NewIdentityID, err)
		}
		newID = pgtype.UUID{Bytes: u, Valid: true}
	}
	rows, err := s.q.ListIdentityAudit(ctx, sqlc.ListIdentityAuditParams{
		Limit:         int32(limit),
		NewIdentityID: newID,
	})
	if err != nil {
		return nil, fmt.Errorf("list identity audit: %w", err)
	}
	out := make([]IdentityAuditRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, identityAuditFromRow(r))
	}
	return out, nil
}

func identityAuditFromRow(r sqlc.AuraIdentityAudit) IdentityAuditRow {
	return IdentityAuditRow{
		ID:                  uuid.UUID(r.ID.Bytes).String(),
		CreatedAt:           r.CreatedAt.Time,
		ActorIdentityID:     r.ActorIdentityID,
		NewIdentityID:       uuid.UUID(r.NewIdentityID.Bytes).String(),
		NewIdentityName:     r.NewIdentityName,
		GrantedCapabilities: r.GrantedCapabilities,
		AuthulaUserID:       r.AuthulaUserID,
	}
}
