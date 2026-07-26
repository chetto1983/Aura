package adaptive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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
		cohort, err := loadCohortForReconstructionTx(
			ctx, q, ownerID, cohortID,
		)
		if err != nil {
			return err
		}
		ledger, err = reconstructCohortAtLookTx(
			ctx, q, cohort, cohort.Cutoff(), closure.Time,
		)
		return err
	})
	if err != nil {
		return CohortLedger{}, fmt.Errorf("reconstruct adaptive focal cohort %s: %w", cohortID, err)
	}
	return ledger, nil
}

func loadCohortForReconstructionTx(
	ctx context.Context,
	queries *sqlc.Queries,
	ownerID uuid.UUID,
	cohortID uuid.UUID,
) (*FocalCohort, error) {
	row, err := queries.GetAdaptiveFocalCohortForReconstruction(
		ctx,
		sqlc.GetAdaptiveFocalCohortForReconstructionParams{
			OwnerID: dbUUID(ownerID), CohortID: dbUUID(cohortID),
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCohortNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read adaptive focal cohort: %w", err)
	}
	return cohortFromPersistedRow(row, ownerID, cohortID)
}

func reconstructCohortAtLookTx(
	ctx context.Context,
	queries *sqlc.Queries,
	cohort *FocalCohort,
	lookCutoff time.Time,
	closureTime time.Time,
) (CohortLedger, error) {
	if closureTime.Before(lookCutoff) {
		return reconstructCohortLedgerAtLook(
			cohort, lookCutoff, closureTime, nil, nil,
		)
	}
	ownerID := cohort.Scope().OwnerID
	cohortID := cohort.ID()
	claimRows, err := queries.ListAdaptiveFocalCohortClaimsForReconstruction(
		ctx,
		sqlc.ListAdaptiveFocalCohortClaimsForReconstructionParams{
			OwnerID: dbUUID(ownerID), CohortID: dbUUID(cohortID),
		},
	)
	if err != nil {
		return CohortLedger{}, fmt.Errorf(
			"read adaptive focal cohort claims: %w", err,
		)
	}
	claims := make([]FocalClaim, 0, len(claimRows))
	for _, claimRow := range claimRows {
		claim, err := focalClaimFromPersistedRow(claimRow)
		if err != nil {
			return CohortLedger{}, fmt.Errorf(
				"%w: persisted claim", ErrInvalidCohortLedger,
			)
		}
		claims = append(claims, claim)
	}
	factRows, err := queries.ListAdaptiveFocalCohortFactsForReconstruction(
		ctx,
		sqlc.ListAdaptiveFocalCohortFactsForReconstructionParams{
			OwnerID: dbUUID(ownerID), CohortID: cohortID.String(),
		},
	)
	if err != nil {
		return CohortLedger{}, fmt.Errorf(
			"read adaptive focal cohort facts: %w", err,
		)
	}
	facts := make([]cohortLedgerFact, 0, len(factRows))
	for _, factRow := range factRows {
		facts = append(facts, cohortLedgerFactFromPersistedRow(factRow))
	}
	ledger, err := reconstructCohortLedgerAtLook(
		cohort, lookCutoff, closureTime, claims, facts,
	)
	if err == nil || !errors.Is(err, ErrInvalidCohortLedger) {
		return ledger, err
	}
	poisonSHA256, hashErr := poisonedCohortLedgerHash(
		cohort, lookCutoff, claims, facts,
	)
	if hashErr != nil {
		return CohortLedger{}, err
	}
	return CohortLedger{
		CohortID: cohort.ID(), CohortSHA256: cohort.SHA256(),
		State: CohortLedgerClosed, SHA256: poisonSHA256,
		PoisonedMemberCount: len(claims),
	}, err
}

func poisonedCohortLedgerHash(
	cohort *FocalCohort,
	lookCutoff time.Time,
	claims []FocalClaim,
	facts []cohortLedgerFact,
) (string, error) {
	type poisonFact struct {
		Event      Event     `json:"event"`
		RecordedAt time.Time `json:"recorded_at"`
	}
	canonicalFacts := make([]poisonFact, len(facts))
	for index, fact := range facts {
		canonicalFacts[index] = poisonFact{
			Event: fact.event, RecordedAt: fact.recordedAt,
		}
	}
	canonical, err := json.Marshal(struct {
		SchemaID     string       `json:"schema_id"`
		Revision     int          `json:"revision"`
		CohortID     string       `json:"cohort_id"`
		CohortSHA256 string       `json:"cohort_sha256"`
		LookCutoff   time.Time    `json:"look_cutoff"`
		Claims       []FocalClaim `json:"claims"`
		Facts        []poisonFact `json:"facts"`
	}{
		SchemaID: "aura.adaptive.poisoned-ledger/v1", Revision: 1,
		CohortID: cohort.ID().String(), CohortSHA256: cohort.SHA256(),
		LookCutoff: lookCutoff, Claims: claims, Facts: canonicalFacts,
	})
	if err != nil {
		return "", fmt.Errorf("hash poisoned adaptive ledger: %w", err)
	}
	return sha256Hex(canonical), nil
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
		recordedAt: canonicalCohortLedgerTime(row.RecordedAt.Time),
	}
}
