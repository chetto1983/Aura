package pimwatch

import (
	"context"
	"errors"
	"time"
)

// sweepBatch bounds what one identity gives up in one pass of the retention job.
const sweepBatch = 1000

// Roster lists the identities the sweep visits.
type Roster interface {
	IdentityIDs(ctx context.Context) ([]string, error)
}

// Expirer removes one bounded batch of an identity's expired verdicts. *Store satisfies it.
type Expirer interface {
	ExpireVerdicts(ctx context.Context, identityID string, now time.Time, batch int) (int, error)
}

// Sweep expires verdicts past each identity's retention. The tables are fail-closed under
// aura_app, so it walks the roster and binds each identity in turn, the shape of assets.DeleteSweep.
type Sweep struct {
	Store      Expirer
	Identities Roster
}

// SweepExpired removes one batch per identity. An identity that fails does not stop the others:
// whatever it left goes on the next run.
func (s Sweep) SweepExpired(ctx context.Context, now time.Time) (int, error) {
	ids, err := s.Identities.IdentityIDs(ctx)
	if err != nil {
		return 0, err
	}
	removed := 0
	var failures []error
	for _, id := range ids {
		n, err := s.Store.ExpireVerdicts(ctx, id, now, sweepBatch)
		removed += n
		if err != nil {
			failures = append(failures, err)
		}
	}
	return removed, errors.Join(failures...)
}
