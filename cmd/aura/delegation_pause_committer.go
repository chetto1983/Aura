// delegation_pause_committer.go is the ONE-transaction PauseAndPark adapter 51-06b Task
// 1 declares as a consumer seam (internal/swarm/delegation_resume.go) and cmd/aura
// satisfies: a claimed delegation worker's AwaitingInput report must open its own
// attributed pause AND park its queue row atomically, or neither. Composes
// *askuser.Store.InsertTx and *documents.PostgresIngestionJobStore.ParkIngestionJobAwaitingInputTx
// over ONE db.WithIdentityTx, mirroring internal/runner/resume_committer.go's
// PoolResumeCommitter exactly (the established cross-store tx-composition idiom in this
// codebase) — never a second concurrency story.
package main

import (
	"context"
	"errors"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/swarm"
	"github.com/jackc/pgx/v5/pgxpool"
)

// errDelegationParkLost is a rollback-only sentinel: the queue row's conditional UPDATE
// matched zero rows (lease already lost between the claim and this call), so the whole
// tx — INCLUDING the just-inserted pause row — must roll back. Never returned to a
// caller; OpenPauseAndPark translates it to (false, nil).
var errDelegationParkLost = errors.New("delegation pause committer: park lost lease")

// delegationPauseCommitter is the production swarm.PauseAndPark: it owns the shared
// *pgxpool.Pool and the concrete askuser/documents Stores. pause.Token identifies the
// tenant (askuser.InsertParams carries no identity field of its own — the row's identity
// comes from the session GUC db.WithIdentityTx sets), so park.IdentityID is the ONE
// source of truth for which identity's tx this runs under.
type delegationPauseCommitter struct {
	pool  *pgxpool.Pool
	pause *askuser.Store
	jobs  *documents.PostgresIngestionJobStore
}

// newDelegationPauseCommitter builds the atomic committer over the shared pool +
// concrete stores. The composition root (newRuntimeDelegationWorker) injects it into
// the claim loop's PauseParker field.
func newDelegationPauseCommitter(pool *pgxpool.Pool, pause *askuser.Store, jobs *documents.PostgresIngestionJobStore) *delegationPauseCommitter {
	return &delegationPauseCommitter{pool: pool, pause: pause, jobs: jobs}
}

// OpenPauseAndPark writes the pause row then parks the queue row, in one tx keyed off
// park.IdentityID. A park RowsAffected==0 (the lease was already lost between the claim
// and this call) rolls the whole tx back — the pause is NEVER left orphaned without its
// parked row, matching swarm.PauseAndPark's own documented contract.
func (c *delegationPauseCommitter) OpenPauseAndPark(ctx context.Context, pause askuser.InsertParams, park documents.ParkAwaitingInputRequest) (bool, error) {
	err := db.WithIdentityTx(ctx, c.pool, park.IdentityID, func(q *sqlc.Queries) error {
		if err := c.pause.InsertTx(ctx, q, pause); err != nil {
			return err
		}
		n, err := c.jobs.ParkIngestionJobAwaitingInputTx(ctx, q, park)
		if err != nil {
			return err
		}
		if n == 0 {
			return errDelegationParkLost
		}
		return nil
	})
	if errors.Is(err, errDelegationParkLost) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

var _ swarm.PauseAndPark = (*delegationPauseCommitter)(nil)
