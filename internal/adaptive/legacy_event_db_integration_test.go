//go:build db_integration

package adaptive

import (
	"context"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

func (s *Store) recordLegacyEvent(ctx context.Context, event Event) (int64, error) {
	sequence, err := s.recordValidated(ctx, event)
	if err != nil {
		return 0, fmt.Errorf("record legacy adaptive event %s: %w", event.ID, err)
	}
	return sequence, nil
}

func (s *Store) recordLegacyEventTx(
	ctx context.Context,
	q *sqlc.Queries,
	event Event,
) (sequence int64, duplicate bool, err error) {
	if s == nil {
		return 0, false, errors.New("legacy adaptive fixture requires a store")
	}
	if q == nil {
		return 0, false, errors.New("legacy adaptive fixture requires database queries")
	}
	return s.recordValidatedTx(ctx, q, event)
}
