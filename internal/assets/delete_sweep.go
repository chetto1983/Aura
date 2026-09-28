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

// uploadGrace is how long past its URL's expiry a presigned upload is still waited for: an
// upload begun just before expiry may still be streaming. An hour carries the largest cap,
// 100 MiB, at about 29 KB/s. Every client finalizes as soon as its PUT returns.
const uploadGrace = time.Hour

// IdentityLister names every identity whose assets a sweep visits.
type IdentityLister interface {
	IdentityIDs(context.Context) ([]string, error)
}

// DeleteSweep finishes, on the retention schedule, the deletes Service.Delete could not: the
// object removal or the row removal that failed, and the rows soft-deleted before a delete
// went further than marking them deleting. It first marks deleting the abandoned uploads,
// presigned rows untouched for the URL's lifetime plus uploadGrace, so they leave the same way.
type DeleteSweep struct {
	Assets     *Service
	Identities IdentityLister
	// Batch overrides deleteSweepBatch; zero keeps it.
	Batch int
}

// SweepExpired retires every identity's uploads abandoned by now, then finishes its oldest
// deleting rows, a bounded batch each, and returns how many left the table or became
// tombstones. What it cannot retire or finish is named in the joined error and retried on the
// next run.
func (d DeleteSweep) SweepExpired(ctx context.Context, now time.Time) (int, error) {
	identities, err := d.Identities.IdentityIDs(ctx)
	if err != nil {
		return 0, fmt.Errorf("asset delete sweep: list identities: %w", err)
	}
	batch := d.Batch
	if batch <= 0 {
		batch = deleteSweepBatch
	}
	abandoned := now.Add(-d.Assets.ttl() - uploadGrace)
	finished := 0
	var errs []error
	for _, identityID := range identities {
		if err := d.Assets.Store.RetireAbandonedUploads(ctx, identityID, abandoned, batch); err != nil {
			errs = append(errs, fmt.Errorf("retire abandoned uploads of %s: %w", identityID, err))
		}
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
