package adaptive

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	// ErrCohortV1RandomizationUnsupported keeps legacy cohorts readable but historical.
	ErrCohortV1RandomizationUnsupported = errors.New(
		"adaptive focal cohort v1 cannot randomize",
	)
)

// CohortStore owns immutable focal cohort persistence and atomic enrollment.
type CohortStore struct {
	pool            *pgxpool.Pool
	ledger          *Store
	entropy         io.Reader
	transact        identityTransactor
	writeAssignment randomizationAssignmentWriter
	writeReceipt    randomizationReceiptWriter
}

// NewCohortStore binds immutable cohorts and schema-2 assignments to PostgreSQL.
func NewCohortStore(pool *pgxpool.Pool, ledger *Store) *CohortStore {
	return &CohortStore{
		pool: pool, ledger: ledger, entropy: rand.Reader,
		transact: db.WithIdentityTx,
		writeAssignment: func(
			ctx context.Context,
			queries *sqlc.Queries,
			event Event,
		) (bool, error) {
			_, duplicate, err := ledger.recordOwnerLockedTx(ctx, queries, event)
			return duplicate, err
		},
		writeReceipt: insertRandomizationReceipt,
	}
}

// Save persists a verified focal cohort exactly once.
func (store *CohortStore) Save(ctx context.Context, cohort *FocalCohort) error {
	artifact, err := verifiedCohortArtifact(cohort)
	if err != nil {
		return err
	}
	var randomizationPlanArtifact []byte
	if cohort.randomizationEligible() {
		randomizationPlanArtifact, err = cohort.canonicalRandomizationPlanArtifact()
		if err != nil {
			return fmt.Errorf("%w: %v", ErrUnverifiedCohort, err)
		}
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
			CohortID:                      dbUUID(cohort.ID()),
			OwnerID:                       dbUUID(scope.OwnerID),
			ProviderID:                    scope.ProviderID,
			ModelID:                       scope.ModelID,
			PolicyEpoch:                   int64(scope.PolicyEpoch),
			PolicyVersion:                 scope.PolicyVersion,
			SnapshotID:                    dbUUID(scope.SnapshotID),
			SnapshotSha256:                snapshotDigest,
			Environment:                   string(scope.Environment),
			Domain:                        string(predicate.Domain),
			DecisionPoint:                 string(predicate.Point),
			PointOrdinal:                  int64(predicate.Ordinal),
			PredicateSha256:               predicateDigest,
			ExperimentID:                  cohort.ExperimentID(),
			Cutoff:                        dbTime(cohort.Cutoff()),
			CohortSha256:                  digest,
			Artifact:                      artifact,
			ArtifactJson:                  artifact,
			RandomizationPlanArtifact:     randomizationPlanArtifact,
			RandomizationPlanArtifactJson: randomizationPlanArtifact,
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
	var header struct {
		SchemaID string `json:"schema_id"`
	}
	if err := json.Unmarshal(row.Artifact, &header); err != nil {
		return nil, fmt.Errorf("%w: decode schema: %v", ErrPersistedCohortInvalid, err)
	}
	if header.SchemaID == CohortSchemaIDV2 {
		return cohortV2FromPersistedRow(row, ownerID, cohortID)
	}
	if header.SchemaID != "" {
		return nil, fmt.Errorf("%w: unsupported schema", ErrPersistedCohortInvalid)
	}
	return legacyCohortFromPersistedRow(row, ownerID, cohortID)
}

func legacyCohortFromPersistedRow(
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
	switch {
	case document.SchemaVersion != CohortSchemaVersion || !reflect.DeepEqual(document, projected):
		return nil, fmt.Errorf("%w: artifact projection mismatch", ErrPersistedCohortInvalid)
	case len(row.RandomizationPlanArtifact) != 0 ||
		len(row.RandomizationPlanArtifactJson) != 0:
		return nil, fmt.Errorf("%w: legacy child artifact mismatch", ErrPersistedCohortInvalid)
	}
	if err := validatePersistedCohortRow(
		row, cohort, artifact, ownerID, cohortID,
	); err != nil {
		return nil, err
	}
	return cohort, nil
}

func cohortV2FromPersistedRow(
	row sqlc.AuraAdaptiveFocalCohorts,
	ownerID uuid.UUID,
	cohortID uuid.UUID,
) (*FocalCohort, error) {
	document, err := decodeCanonicalCohortV2(row.Artifact)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPersistedCohortInvalid, err)
	}
	plan, err := decodeCanonicalRandomizationPlan(row.RandomizationPlanArtifact)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: randomization plan: %v", ErrPersistedCohortInvalid, err,
		)
	}
	cohort, err := newFocalCohortV2(document, plan)
	if err != nil {
		return nil, fmt.Errorf("%w: reconstruct v2: %v", ErrPersistedCohortInvalid, err)
	}
	artifact, err := cohort.canonicalArtifact()
	if err != nil {
		return nil, fmt.Errorf("%w: canonicalize v2: %v", ErrPersistedCohortInvalid, err)
	}
	planArtifact, err := cohort.canonicalRandomizationPlanArtifact()
	if err != nil {
		return nil, fmt.Errorf(
			"%w: canonicalize randomization plan: %v", ErrPersistedCohortInvalid, err,
		)
	}
	switch {
	case !jsonDocumentsEqual(row.Artifact, row.ArtifactJson) ||
		!jsonDocumentsEqual(
			row.RandomizationPlanArtifact, row.RandomizationPlanArtifactJson,
		):
		return nil, fmt.Errorf("%w: v2 artifact projection mismatch", ErrPersistedCohortInvalid)
	case !bytes.Equal(row.RandomizationPlanArtifact, planArtifact):
		return nil, fmt.Errorf(
			"%w: randomization plan is not canonical", ErrPersistedCohortInvalid,
		)
	}
	if err := validatePersistedCohortRow(
		row, cohort, artifact, ownerID, cohortID,
	); err != nil {
		return nil, err
	}
	return cohort, nil
}

func validatePersistedCohortRow(
	row sqlc.AuraAdaptiveFocalCohorts,
	cohort *FocalCohort,
	artifact []byte,
	ownerID uuid.UUID,
	cohortID uuid.UUID,
) error {
	scope := cohort.Scope()
	predicate := cohort.Predicate()
	digest, digestErr := hex.DecodeString(cohort.SHA256())
	predicateDigest, predicateErr := hex.DecodeString(cohort.PredicateSHA256())
	snapshotDigest, snapshotErr := hex.DecodeString(scope.SnapshotSHA256)
	rowOwner := uuid.UUID(row.OwnerID.Bytes)
	rowID := uuid.UUID(row.ID.Bytes)
	switch {
	case !row.OwnerID.Valid || rowOwner != ownerID || scope.OwnerID != ownerID:
		return fmt.Errorf("%w: owner mismatch", ErrPersistedCohortInvalid)
	case !row.ID.Valid || rowID != cohortID || cohort.ID() != cohortID:
		return fmt.Errorf("%w: cohort identity mismatch", ErrPersistedCohortInvalid)
	case digestErr != nil || !bytes.Equal(row.Sha256, digest):
		return fmt.Errorf("%w: digest mismatch", ErrPersistedCohortInvalid)
	case predicateErr != nil || !bytes.Equal(row.PredicateSha256, predicateDigest):
		return fmt.Errorf("%w: predicate digest mismatch", ErrPersistedCohortInvalid)
	case snapshotErr != nil || !bytes.Equal(row.SnapshotSha256, snapshotDigest):
		return fmt.Errorf("%w: snapshot digest mismatch", ErrPersistedCohortInvalid)
	case row.ProviderID != scope.ProviderID || row.ModelID != scope.ModelID ||
		row.PolicyEpoch != int64(scope.PolicyEpoch) || row.PolicyVersion != scope.PolicyVersion ||
		row.Environment != string(scope.Environment):
		return fmt.Errorf("%w: scope mismatch", ErrPersistedCohortInvalid)
	case !row.SnapshotID.Valid || uuid.UUID(row.SnapshotID.Bytes) != scope.SnapshotID:
		return fmt.Errorf("%w: snapshot identity mismatch", ErrPersistedCohortInvalid)
	case row.Domain != string(predicate.Domain) || row.DecisionPoint != string(predicate.Point) ||
		row.PointOrdinal != int64(predicate.Ordinal) || row.ExperimentID != cohort.ExperimentID():
		return fmt.Errorf("%w: predicate mismatch", ErrPersistedCohortInvalid)
	case !row.Cutoff.Valid || !row.Cutoff.Time.Equal(cohort.Cutoff()):
		return fmt.Errorf("%w: cutoff mismatch", ErrPersistedCohortInvalid)
	case !bytes.Equal(row.Artifact, artifact):
		return fmt.Errorf("%w: artifact is not canonical", ErrPersistedCohortInvalid)
	}
	return nil
}

func decodeCanonicalCohort(artifact []byte) (canonicalCohort, error) {
	var document canonicalCohort
	if err := decodeStrictJSON(artifact, &document); err != nil {
		return canonicalCohort{}, err
	}
	return document, nil
}

func decodeCanonicalCohortV2(artifact []byte) (canonicalCohortV2, error) {
	var document canonicalCohortV2
	if err := decodeStrictJSON(artifact, &document); err != nil {
		return canonicalCohortV2{}, err
	}
	canonical, err := json.Marshal(document)
	if err != nil || !bytes.Equal(canonical, artifact) {
		return canonicalCohortV2{}, errors.New("adaptive cohort v2 encoding is noncanonical")
	}
	return document, nil
}

func decodeCanonicalRandomizationPlan(
	artifact []byte,
) (canonicalRandomizationPlan, error) {
	var plan canonicalRandomizationPlan
	if err := decodeStrictJSON(artifact, &plan); err != nil {
		return canonicalRandomizationPlan{}, err
	}
	canonical, err := json.Marshal(plan)
	if err != nil || !bytes.Equal(canonical, artifact) {
		return canonicalRandomizationPlan{}, errors.New(
			"adaptive randomization plan encoding is noncanonical",
		)
	}
	return plan, nil
}

func jsonDocumentsEqual(left, right []byte) bool {
	decode := func(payload []byte) (any, error) {
		var document any
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.UseNumber()
		if err := decoder.Decode(&document); err != nil {
			return nil, err
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			return nil, errors.New("JSON projection has trailing data")
		}
		return document, nil
	}
	leftDocument, leftErr := decode(left)
	rightDocument, rightErr := decode(right)
	return leftErr == nil && rightErr == nil &&
		reflect.DeepEqual(leftDocument, rightDocument)
}
