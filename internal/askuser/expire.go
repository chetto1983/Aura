package askuser

import (
	"context"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

// ListExpiredPendingApprovals returns due approval pauses for the identity on ctx.
// It only selects candidates; Runner performs each conditional claim together with
// its matching RoleTool answer through ResumeCommitter.
func (s *Store) ListExpiredPendingApprovals(ctx context.Context, cutoff time.Time, limit int) ([]Pending, error) {
	lim := normalizeLimit(limit, 100)
	var out []Pending
	if err := s.scoped(ctx, func(q *sqlc.Queries) error {
		rows, err := q.ListExpiredPendingApprovals(ctx, sqlc.ListExpiredPendingApprovalsParams{
			CreatedAt: pgtype.Timestamptz{Time: cutoff, Valid: true},
			Limit:     lim,
		})
		if err != nil {
			return err
		}
		out = make([]Pending, 0, len(rows))
		for _, row := range rows {
			out = append(out, fromRow(row))
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("list expired pending approvals: %w", err)
	}
	return out, nil
}
