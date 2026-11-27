package messagedrafts

import (
	"context"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
)

// interruptedAfter is how long a dispatch may run before its outcome counts as unknowable: past it
// the draft is uncertain, never dispatched again and never returned to pending.
const interruptedAfter = 30 * time.Minute

// Recovered counts what one Recover changed.
type Recovered struct {
	Expired, Interrupted int
}

// Recover does to every draft of one owner what Reconcile does to the draft it reads: it expires the
// immediate reviews past their expiry and makes the dispatches older than interruptedAfter uncertain.
// It is the sweep for the drafts nobody reopens, which otherwise stay open for good (on the lab VM a
// review stayed pending 46 days past its expiry). It never sends and never grants a send; a scheduled
// draft is left to its task.
func (s *Store) Recover(ctx context.Context, ownerID string) (Recovered, error) {
	owner, err := db.ParseUUID("owner id", ownerID)
	if err != nil {
		return Recovered{}, ErrUnavailable
	}
	var got Recovered
	err = db.WithIdentityTx(ctx, s.pool, ownerID, func(q *sqlc.Queries) error {
		expired, err := q.ExpireStaleMessageDrafts(ctx, owner)
		if err != nil {
			return err
		}
		interrupted, err := q.MarkAbandonedMessageDraftDispatchesUncertain(ctx, sqlc.MarkAbandonedMessageDraftDispatchesUncertainParams{
			IdentityID: owner, InterruptedAfterSecs: interruptedAfter.Seconds(),
		})
		if err != nil {
			return err
		}
		got = Recovered{Expired: len(expired), Interrupted: len(interrupted)}
		return nil
	})
	return got, err
}
