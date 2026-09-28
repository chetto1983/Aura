package assets

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// deleteSweepBatch bounds one identity's share of a sweep. It drains the 258-row backlog
// measured on the lab VM (2026-09-28) in one nightly run while keeping a run far inside the
// retention job's five minutes: each row costs one object delete and one transaction.
const deleteSweepBatch = 500

// IdentityLister names every identity whose assets a sweep visits.
type IdentityLister interface {
	IdentityIDs(context.Context) ([]string, error)
}

// DeleteSweep finishes, on the retention schedule, the deletes Service.Delete could not: the
// object removal or the row removal that failed, and the rows soft-deleted before a delete
// went further than marking them deleting.
type DeleteSweep struct {
	Assets     *Service
	Identities IdentityLister
	// Batch overrides deleteSweepBatch; zero keeps it.
	Batch int
}

// SweepExpired finishes the oldest deleting rows of every identity, a bounded batch each, and
// returns how many left the table or became tombstones. A row it cannot finish is named in
// the joined error and retried on the next run. Every deleting row is due, so now is unused.
func (d DeleteSweep) SweepExpired(ctx context.Context, _ time.Time) (int, error) {
	identities, err := d.Identities.IdentityIDs(ctx)
	if err != nil {
		return 0, fmt.Errorf("asset delete sweep: list identities: %w", err)
	}
	batch := d.Batch
	if batch <= 0 {
		batch = deleteSweepBatch
	}
	finished := 0
	var errs []error
	for _, identityID := range identities {
		n, err := d.Assets.finishDeletes(ctx, identityID, batch)
		finished += n
		if err != nil {
			errs = append(errs, err)
		}
	}
	return finished, errors.Join(errs...)
}

func (s *Service) finishDeletes(ctx context.Context, identityID string, limit int) (int, error) {
	pending, err := s.Store.ListDeleting(ctx, identityID, limit)
	if err != nil {
		return 0, fmt.Errorf("list deleting assets of %s: %w", identityID, err)
	}
	finished := 0
	var errs []error
	for _, asset := range pending {
		if err := s.finishDelete(ctx, asset); err != nil {
			errs = append(errs, fmt.Errorf("finish delete of asset %s: %w", asset.ID, err))
			continue
		}
		finished++
	}
	return finished, errors.Join(errs...)
}
