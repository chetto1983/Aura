package adaptive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrUnverifiedCohort rejects nil, forged, or internally inconsistent cohorts.
	ErrUnverifiedCohort = errors.New("adaptive focal cohort is not verified")
	// ErrCohortConflict reports reuse of a cohort identity with different content.
	ErrCohortConflict = errors.New("adaptive focal cohort conflicts with persisted content")
	// ErrCohortNotFound hides absent and foreign owner-scoped cohorts identically.
	ErrCohortNotFound = errors.New("adaptive focal cohort not found")
	// ErrPersistedCohortInvalid rejects storage that no longer matches its typed artifact.
	ErrPersistedCohortInvalid = errors.New("persisted adaptive focal cohort is invalid")
)

// CohortStore owns immutable focal cohort persistence and atomic enrollment.
type CohortStore struct {
	pool   *pgxpool.Pool
	ledger *Store
}

// NewCohortStore binds immutable cohorts and schema-2 assignments to PostgreSQL.
func NewCohortStore(pool *pgxpool.Pool, ledger *Store) *CohortStore {
	return &CohortStore{pool: pool, ledger: ledger}
}

// Save persists a verified focal cohort exactly once.
func (store *CohortStore) Save(ctx context.Context, cohort *FocalCohort) error {
	artifact, err := verifiedCohortArtifact(cohort)
	if err != nil {
		return err
	}
	if store == nil || store.pool == nil {
		return errors.New("adaptive cohort store requires a database pool")
	}
	scope := cohort.Scope()
	predicate := cohort.Predicate()
	digest, err := hex.DecodeString(cohort.SHA256())
	if err != nil {
		return fmt.Errorf("%w: decode cohort digest: %v", ErrUnverifiedCohort, err)
	}
	predicateDigest, err := hex.DecodeString(cohort.PredicateSHA256())
	if err != nil {
		return fmt.Errorf("%w: decode predicate digest: %v", ErrUnverifiedCohort, err)
	}
	snapshotDigest, err := hex.DecodeString(scope.SnapshotSHA256)
	if err != nil {
		return fmt.Errorf("%w: decode snapshot digest: %v", ErrUnverifiedCohort, err)
	}
	err = db.WithIdentityTx(ctx, store.pool, scope.OwnerID.String(), func(q *sqlc.Queries) error {
		if err := lockAdaptiveOwnerTx(ctx, q, scope.OwnerID); err != nil {
			return err
		}
		rows, err := q.InsertAdaptiveFocalCohort(ctx, sqlc.InsertAdaptiveFocalCohortParams{
			CohortID:        dbUUID(cohort.ID()),
			OwnerID:         dbUUID(scope.OwnerID),
			ProviderID:      scope.ProviderID,
			ModelID:         scope.ModelID,
			PolicyEpoch:     int64(scope.PolicyEpoch),
			PolicyVersion:   scope.PolicyVersion,
			SnapshotID:      dbUUID(scope.SnapshotID),
			SnapshotSha256:  snapshotDigest,
			Environment:     string(scope.Environment),
			Domain:          string(predicate.Domain),
			DecisionPoint:   string(predicate.Point),
			PointOrdinal:    int64(predicate.Ordinal),
			PredicateSha256: predicateDigest,
			ExperimentID:    cohort.ExperimentID(),
			Cutoff:          dbTime(cohort.Cutoff()),
			CohortSha256:    digest,
			Artifact:        artifact,
			ArtifactJson:    artifact,
		})
		if err != nil {
			return fmt.Errorf("insert adaptive focal cohort: %w", err)
		}
		row, err := q.GetAdaptiveFocalCohort(ctx, sqlc.GetAdaptiveFocalCohortParams{
			OwnerID:  dbUUID(scope.OwnerID),
			CohortID: dbUUID(cohort.ID()),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrCohortConflict
		}
		if err != nil {
			return fmt.Errorf("read replayed adaptive focal cohort: %w", err)
		}
		persisted, err := cohortFromPersistedRow(row, scope.OwnerID, cohort.ID())
		if err != nil || persisted.SHA256() != cohort.SHA256() || !bytes.Equal(row.Artifact, artifact) {
			if rows == 1 && err != nil {
				return err
			}
			if rows == 1 {
				return ErrPersistedCohortInvalid
			}
			return ErrCohortConflict
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("save adaptive focal cohort %s: %w", cohort.ID(), err)
	}
	return nil
}

// Load reconstructs one verified owner-scoped cohort from PostgreSQL.
func (store *CohortStore) Load(
	ctx context.Context,
	ownerID uuid.UUID,
	cohortID uuid.UUID,
) (*FocalCohort, error) {
	if ownerID == uuid.Nil || cohortID == uuid.Nil {
		return nil, ErrCohortNotFound
	}
	if store == nil || store.pool == nil {
		return nil, errors.New("adaptive cohort store requires a database pool")
	}
	row, err := store.getPersistedCohort(ctx, ownerID, cohortID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrCohortNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load adaptive focal cohort %s: %w", cohortID, err)
	}
	cohort, err := cohortFromPersistedRow(row, ownerID, cohortID)
	if err != nil {
		return nil, fmt.Errorf("load adaptive focal cohort %s: %w", cohortID, err)
	}
	return cohort, nil
}

func (store *CohortStore) getPersistedCohort(
	ctx context.Context,
	ownerID uuid.UUID,
	cohortID uuid.UUID,
) (sqlc.AuraAdaptiveFocalCohorts, error) {
	var row sqlc.AuraAdaptiveFocalCohorts
	err := db.WithIdentityTx(ctx, store.pool, ownerID.String(), func(q *sqlc.Queries) error {
		if err := lockAdaptiveOwnerTx(ctx, q, ownerID); err != nil {
			return fmt.Errorf("guard focal cohort owner: %w", err)
		}
		var err error
		row, err = q.GetAdaptiveFocalCohort(ctx, sqlc.GetAdaptiveFocalCohortParams{
			OwnerID: dbUUID(ownerID), CohortID: dbUUID(cohortID),
		})
		return err
	})
	return row, err
}

func verifiedCohortArtifact(cohort *FocalCohort) ([]byte, error) {
	artifact, err := cohort.canonicalArtifact()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnverifiedCohort, err)
	}
	sum := sha256.Sum256(artifact)
	if cohort.SHA256() != hex.EncodeToString(sum[:]) {
		return nil, ErrUnverifiedCohort
	}
	return artifact, nil
}

func cohortFromPersistedRow(
	row sqlc.AuraAdaptiveFocalCohorts,
	ownerID uuid.UUID,
	cohortID uuid.UUID,
) (*FocalCohort, error) {
	document, err := decodeCanonicalCohort(row.Artifact)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPersistedCohortInvalid, err)
	}
	projected, err := decodeCanonicalCohort(row.ArtifactJson)
	if err != nil {
		return nil, fmt.Errorf("%w: artifact projection: %v", ErrPersistedCohortInvalid, err)
	}
	registrations := make([]EvaluatorRegistration, len(document.Evaluators))
	for index, evaluator := range document.Evaluators {
		registrations[index] = EvaluatorRegistration(evaluator)
	}
	cohort, err := NewFocalCohort(CohortSpec{
		Scope:          document.Scope,
		Predicate:      document.Predicate,
		ExperimentID:   document.ExperimentID,
		Arms:           document.Arms,
		Actions:        document.Actions,
		Evaluators:     registrations,
		PrimaryQuality: document.PrimaryQuality,
		PrimaryHarm:    document.PrimaryHarm,
		Power:          document.Power,
		Margins:        document.Margins,
		Censoring:      document.Censoring,
		Admission:      document.Admission,
		Looks:          document.Looks,
		Cutoff:         document.Cutoff,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: reconstruct: %v", ErrPersistedCohortInvalid, err)
	}
	artifact, err := cohort.canonicalArtifact()
	if err != nil {
		return nil, fmt.Errorf("%w: canonicalize: %v", ErrPersistedCohortInvalid, err)
	}
	scope := cohort.Scope()
	predicate := cohort.Predicate()
	digest, digestErr := hex.DecodeString(cohort.SHA256())
	predicateDigest, predicateErr := hex.DecodeString(cohort.PredicateSHA256())
	snapshotDigest, snapshotErr := hex.DecodeString(scope.SnapshotSHA256)
	rowOwner := uuid.UUID(row.OwnerID.Bytes)
	rowID := uuid.UUID(row.ID.Bytes)
	switch {
	case document.SchemaVersion != CohortSchemaVersion || !reflect.DeepEqual(document, projected):
		return nil, fmt.Errorf("%w: artifact projection mismatch", ErrPersistedCohortInvalid)
	case !row.OwnerID.Valid || rowOwner != ownerID || scope.OwnerID != ownerID:
		return nil, fmt.Errorf("%w: owner mismatch", ErrPersistedCohortInvalid)
	case !row.ID.Valid || rowID != cohortID || cohort.ID() != cohortID:
		return nil, fmt.Errorf("%w: cohort identity mismatch", ErrPersistedCohortInvalid)
	case digestErr != nil || !bytes.Equal(row.Sha256, digest):
		return nil, fmt.Errorf("%w: digest mismatch", ErrPersistedCohortInvalid)
	case predicateErr != nil || !bytes.Equal(row.PredicateSha256, predicateDigest):
		return nil, fmt.Errorf("%w: predicate digest mismatch", ErrPersistedCohortInvalid)
	case snapshotErr != nil || !bytes.Equal(row.SnapshotSha256, snapshotDigest):
		return nil, fmt.Errorf("%w: snapshot digest mismatch", ErrPersistedCohortInvalid)
	case row.ProviderID != scope.ProviderID || row.ModelID != scope.ModelID ||
		row.PolicyEpoch != int64(scope.PolicyEpoch) || row.PolicyVersion != scope.PolicyVersion ||
		row.Environment != string(scope.Environment):
		return nil, fmt.Errorf("%w: scope mismatch", ErrPersistedCohortInvalid)
	case !row.SnapshotID.Valid || uuid.UUID(row.SnapshotID.Bytes) != scope.SnapshotID:
		return nil, fmt.Errorf("%w: snapshot identity mismatch", ErrPersistedCohortInvalid)
	case row.Domain != string(predicate.Domain) || row.DecisionPoint != string(predicate.Point) ||
		row.PointOrdinal != int64(predicate.Ordinal) || row.ExperimentID != cohort.ExperimentID():
		return nil, fmt.Errorf("%w: predicate mismatch", ErrPersistedCohortInvalid)
	case !row.Cutoff.Valid || !row.Cutoff.Time.Equal(cohort.Cutoff()):
		return nil, fmt.Errorf("%w: cutoff mismatch", ErrPersistedCohortInvalid)
	case !bytes.Equal(row.Artifact, artifact):
		return nil, fmt.Errorf("%w: artifact is not canonical", ErrPersistedCohortInvalid)
	}
	return cohort, nil
}

func decodeCanonicalCohort(artifact []byte) (canonicalCohort, error) {
	var document canonicalCohort
	if err := decodeStrictJSON(artifact, &document); err != nil {
		return canonicalCohort{}, err
	}
	return document, nil
}
