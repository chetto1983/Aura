package adaptive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Reconstruct returns the bitemporal cohort view at one database-authoritative
// statement timestamp. It never writes cohort artifacts while rebuilding.
func (store *CohortStore) Reconstruct(
	ctx context.Context,
	ownerID uuid.UUID,
	cohortID uuid.UUID,
) (CohortLedger, error) {
	if ownerID == uuid.Nil || cohortID == uuid.Nil {
		return CohortLedger{}, ErrCohortNotFound
	}
	if store == nil || store.pool == nil {
		return CohortLedger{}, errors.New("adaptive cohort store requires a database pool")
	}
	var ledger CohortLedger
	err := db.WithIdentityTx(ctx, store.pool, ownerID.String(), func(q *sqlc.Queries) error {
		if _, err := q.LockAdaptiveOwnerForCohortReconstruction(ctx, dbUUID(ownerID)); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrCohortNotFound
			}
			return fmt.Errorf("lock adaptive cohort owner: %w", err)
		}
		tombstoned, err := q.AdaptiveIdentityTombstoned(ctx, dbUUID(ownerID))
		if err != nil {
			return fmt.Errorf("check adaptive owner tombstone: %w", err)
		}
		if tombstoned {
			return ErrOwnerTombstoned
		}
		closure, err := q.AdaptiveCohortReconstructionTime(ctx)
		if err != nil {
			return fmt.Errorf("read cohort reconstruction time: %w", err)
		}
		row, err := q.GetAdaptiveFocalCohortForReconstruction(ctx, sqlc.GetAdaptiveFocalCohortForReconstructionParams{
			OwnerID: dbUUID(ownerID), CohortID: dbUUID(cohortID),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrCohortNotFound
		}
		if err != nil {
			return fmt.Errorf("read adaptive focal cohort: %w", err)
		}
		cohort, err := cohortFromPersistedRow(row, ownerID, cohortID)
		if err != nil {
			return err
		}
		if closure.Time.Before(cohort.Cutoff()) {
			ledger, err = reconstructCohortLedger(cohort, closure.Time, nil, nil)
			return err
		}
		claimRows, err := q.ListAdaptiveFocalCohortClaimsForReconstruction(ctx, sqlc.ListAdaptiveFocalCohortClaimsForReconstructionParams{
			OwnerID: dbUUID(ownerID), CohortID: dbUUID(cohortID),
		})
		if err != nil {
			return fmt.Errorf("read adaptive focal cohort claims: %w", err)
		}
		claims := make([]FocalClaim, 0, len(claimRows))
		for _, claimRow := range claimRows {
			claim, err := focalClaimFromPersistedRow(claimRow)
			if err != nil {
				return fmt.Errorf("%w: persisted claim", ErrInvalidCohortLedger)
			}
			claims = append(claims, claim)
		}
		factRows, err := q.ListAdaptiveFocalCohortFactsForReconstruction(ctx, sqlc.ListAdaptiveFocalCohortFactsForReconstructionParams{
			OwnerID: dbUUID(ownerID), CohortID: cohortID.String(),
		})
		if err != nil {
			return fmt.Errorf("read adaptive focal cohort facts: %w", err)
		}
		facts := make([]cohortLedgerFact, 0, len(factRows))
		for _, factRow := range factRows {
			facts = append(facts, cohortLedgerFactFromPersistedRow(factRow))
		}
		ledger, err = reconstructCohortLedger(cohort, closure.Time, claims, facts)
		return err
	})
	if err != nil {
		return CohortLedger{}, fmt.Errorf("reconstruct adaptive focal cohort %s: %w", cohortID, err)
	}
	return ledger, nil
}

func cohortLedgerFactFromPersistedRow(row sqlc.AuraAdaptiveOutbox) cohortLedgerFact {
	return cohortLedgerFact{
		event: Event{
			ID:          uuid.UUID(row.ID.Bytes),
			OwnerID:     uuid.UUID(row.OwnerID.Bytes),
			AggregateID: row.AggregateID,
			DecisionID:  uuid.UUID(row.DecisionID.Bytes),
			Kind:        EventKind(row.EventKind),
			Payload:     json.RawMessage(row.Payload),
			PayloadHash: row.PayloadHash,
			CreatedAt:   row.CreatedAt.Time,
		},
		recordedAt: row.RecordedAt.Time,
	}
}
