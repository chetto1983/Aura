package adaptive

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EvidenceSealer identifies the trusted capability used by the sole writer.
type EvidenceSealer struct {
	ActorID    uuid.UUID
	Capability string
}

func (sealer EvidenceSealer) validate(ownerID uuid.UUID) error {
	if ownerID == uuid.Nil || sealer.ActorID != ownerID ||
		sealer.Capability != "adaptive.evidence.seal" {
		return errors.New("adaptive evidence sealer is invalid")
	}
	return nil
}

// EvidenceStore owns immutable admission and closed-look evidence.
type EvidenceStore struct {
	pool     *pgxpool.Pool
	transact identityTransactor
}

// NewEvidenceStore binds evidence sealing to PostgreSQL.
func NewEvidenceStore(pool *pgxpool.Pool) *EvidenceStore {
	return &EvidenceStore{pool: pool, transact: db.WithIdentityTx}
}

// SealCanaryAdmission builds admission evidence only from stored cohort truth.
func (store *EvidenceStore) SealCanaryAdmission(
	ctx context.Context,
	ownerID uuid.UUID,
	cohortID uuid.UUID,
	sealer EvidenceSealer,
) (CanaryAdmissionEvidence, error) {
	if store == nil || store.pool == nil || store.transact == nil {
		return CanaryAdmissionEvidence{}, errors.New(
			"adaptive evidence store requires a database pool",
		)
	}
	if err := sealer.validate(ownerID); err != nil {
		return CanaryAdmissionEvidence{}, err
	}
	if cohortID == uuid.Nil {
		return CanaryAdmissionEvidence{}, errors.New(
			"adaptive evidence cohort is invalid",
		)
	}
	var evidence CanaryAdmissionEvidence
	prepared := false
	err := store.transact(
		ctx, store.pool, ownerID.String(),
		func(queries *sqlc.Queries) error {
			var err error
			evidence, err = sealCanaryAdmissionTx(
				ctx, queries, ownerID, cohortID, sealer,
			)
			prepared = err == nil
			return err
		},
	)
	if err != nil {
		if prepared {
			recovered, recoveryErr := store.reloadCanaryAdmission(
				ctx, ownerID, cohortID,
			)
			if recoveryErr != nil {
				return CanaryAdmissionEvidence{}, fmt.Errorf(
					"recover adaptive canary admission after unresolved commit: %w",
					recoveryErr,
				)
			}
			if recovered != nil {
				return *recovered, nil
			}
		}
		return CanaryAdmissionEvidence{}, fmt.Errorf(
			"seal adaptive canary admission: %w", err,
		)
	}
	return evidence, nil
}

// RegisterCohortArtifact stores one preregistered non-outcome child exactly once.
func (store *EvidenceStore) RegisterCohortArtifact(
	ctx context.Context,
	ownerID uuid.UUID,
	cohortID uuid.UUID,
	sealer EvidenceSealer,
	registration EvidenceArtifactRegistration,
) error {
	if store == nil || store.pool == nil || store.transact == nil {
		return errors.New("adaptive evidence store requires a database pool")
	}
	if err := sealer.validate(ownerID); err != nil {
		return err
	}
	allowedKinds := []string{
		"interference_plan", "look_plan", "quality_outcome_model",
		"harm_outcome_model", "ope_plan", "operator_approval",
	}
	if err := registration.validate(allowedKinds...); err != nil {
		return err
	}
	return store.transact(
		ctx, store.pool, ownerID.String(),
		func(queries *sqlc.Queries) error {
			if err := lockAdaptiveOwnerTx(ctx, queries, ownerID); err != nil {
				return err
			}
			cohort, err := loadCohortForReconstructionTx(
				ctx, queries, ownerID, cohortID,
			)
			if err != nil {
				return err
			}
			if err := validateRegisteredCohortArtifact(
				cohort, registration,
			); err != nil {
				return err
			}
			return storeEvidenceArtifactTx(
				ctx, queries, ownerID, cohortID, sealer, registration,
			)
		},
	)
}

// SealCanaryOutcome is the sole outcome writer. Its signature deliberately has
// no caller-supplied ledger, report, hash, eligibility, or disposition.
func (store *EvidenceStore) SealCanaryOutcome(
	ctx context.Context,
	ownerID uuid.UUID,
	cohortID uuid.UUID,
	lookNumber int,
	sealer EvidenceSealer,
) (CanaryOutcomeEvidence, error) {
	if store == nil || store.pool == nil || store.transact == nil {
		return CanaryOutcomeEvidence{}, errors.New(
			"adaptive evidence store requires a database pool",
		)
	}
	if err := sealer.validate(ownerID); err != nil {
		return CanaryOutcomeEvidence{}, err
	}
	if cohortID == uuid.Nil || lookNumber < 1 || lookNumber > 5 {
		return CanaryOutcomeEvidence{}, errors.New("adaptive evidence look is invalid")
	}
	if err := ctx.Err(); err != nil {
		return CanaryOutcomeEvidence{}, err
	}
	var evidence CanaryOutcomeEvidence
	prepared := false
	err := store.transact(
		ctx, store.pool, ownerID.String(),
		func(queries *sqlc.Queries) error {
			var err error
			evidence, err = sealCanaryOutcomeTx(
				ctx, queries, ownerID, cohortID, lookNumber, sealer,
			)
			prepared = err == nil
			return err
		},
	)
	if err != nil {
		if prepared {
			recovered, recoveryErr := store.reloadCanaryOutcome(
				ctx, ownerID, cohortID, lookNumber,
			)
			if recoveryErr != nil {
				return CanaryOutcomeEvidence{}, fmt.Errorf(
					"recover adaptive canary outcome after unresolved commit: %w",
					recoveryErr,
				)
			}
			if recovered != nil {
				return *recovered, nil
			}
		}
		return CanaryOutcomeEvidence{}, fmt.Errorf(
			"seal adaptive canary outcome: %w", err,
		)
	}
	return evidence, nil
}

func (store *EvidenceStore) reloadCanaryAdmission(
	ctx context.Context,
	ownerID uuid.UUID,
	cohortID uuid.UUID,
) (*CanaryAdmissionEvidence, error) {
	var evidence *CanaryAdmissionEvidence
	err := db.WithIdentityTx(
		ctx, store.pool, ownerID.String(),
		func(queries *sqlc.Queries) error {
			row, err := queries.GetAdaptiveSealedAdmission(
				ctx,
				sqlc.GetAdaptiveSealedAdmissionParams{
					OwnerID: dbUUID(ownerID), CohortID: dbUUID(cohortID),
				},
			)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			loaded, err := admissionEvidenceFromRow(row, ownerID, cohortID)
			if err == nil {
				evidence = &loaded
			}
			return err
		},
	)
	return evidence, err
}

func (store *EvidenceStore) reloadCanaryOutcome(
	ctx context.Context,
	ownerID uuid.UUID,
	cohortID uuid.UUID,
	lookNumber int,
) (*CanaryOutcomeEvidence, error) {
	var evidence *CanaryOutcomeEvidence
	err := db.WithIdentityTx(
		ctx, store.pool, ownerID.String(),
		func(queries *sqlc.Queries) error {
			row, err := queries.GetAdaptiveSealedOutcomeLook(
				ctx,
				sqlc.GetAdaptiveSealedOutcomeLookParams{
					OwnerID: dbUUID(ownerID), CohortID: dbUUID(cohortID),
					LookNumber: pgtype.Int4{
						Int32: int32(lookNumber), Valid: true,
					},
				},
			)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			loaded, err := outcomeEvidenceFromRow(
				row, ownerID, cohortID, lookNumber,
			)
			if err == nil {
				evidence = &loaded
			}
			return err
		},
	)
	return evidence, err
}

func validateRegisteredCohortArtifact(
	cohort *FocalCohort,
	registration EvidenceArtifactRegistration,
) error {
	if cohort == nil || !cohort.randomizationEligible() {
		return ErrCohortV1RandomizationUnsupported
	}
	document := cohort.v2.document
	var expected ChildArtifactRef
	switch registration.Ref.Kind {
	case "interference_plan":
		expected = document.InterferencePlanRef
		plan, err := DecodeInterferencePlanArtifact(registration.Canonical)
		cluster, clusterErr := NewInterferenceCluster(
			document.OwnerID, cohort.ID().String(),
		)
		if err != nil || clusterErr != nil ||
			plan.ClusterSchemaSHA256 != cluster.SchemaSHA256 {
			return errors.New(
				"adaptive interference plan differs from cohort",
			)
		}
	case "look_plan":
		expected = document.LookPlanRef
		lookPlan, err := DecodeEvidenceLookPlanArtifact(registration.Canonical)
		if err != nil || !lookPlan.Looks[4].CutoffAt.Equal(cohort.Cutoff()) {
			return errors.New("adaptive evidence look plan differs from cohort")
		}
	case "quality_outcome_model":
		expected = document.QualityOutcomeModelRef
	case "harm_outcome_model":
		expected = document.HarmOutcomeModelRef
	case "ope_plan":
		expected = document.OPEPlanRef
	case "operator_approval":
		approval, err := DecodeOperatorApproval(registration.Canonical)
		if err != nil || approval.OwnerID != document.OwnerID ||
			approval.CohortID != cohort.ID().String() ||
			approval.CohortSHA256 != cohort.SHA256() ||
			approval.PolicyEpoch != document.PolicyEpoch ||
			approval.PolicyVersion != document.PolicyVersion {
			return errors.New("adaptive operator approval differs from cohort")
		}
		return nil
	default:
		return errors.New("adaptive evidence artifact kind is invalid")
	}
	if registration.Ref != expected {
		return errors.New("adaptive evidence artifact reference differs from cohort")
	}
	return nil
}

func storeEvidenceArtifactTx(
	ctx context.Context,
	queries *sqlc.Queries,
	ownerID uuid.UUID,
	cohortID uuid.UUID,
	sealer EvidenceSealer,
	registration EvidenceArtifactRegistration,
) error {
	digest, err := hex.DecodeString(registration.Ref.ArtifactSHA256)
	if err != nil {
		return err
	}
	artifactID, err := uuid.Parse(registration.Ref.ArtifactID)
	if err != nil {
		return err
	}
	row, err := queries.StoreAdaptiveEvidenceArtifact(
		ctx,
		sqlc.StoreAdaptiveEvidenceArtifactParams{
			ActorID: dbUUID(sealer.ActorID), OwnerID: dbUUID(ownerID),
			CohortID: dbUUID(cohortID), Kind: registration.Ref.Kind,
			ArtifactID:     dbUUID(artifactID),
			SchemaID:       registration.Ref.ArtifactSchemaID,
			Revision:       int32(registration.Ref.ArtifactRevision),
			ArtifactSha256: digest, Artifact: registration.Canonical,
			ArtifactJson: registration.Canonical,
		},
	)
	if err != nil {
		return fmt.Errorf("store adaptive evidence artifact: %w", err)
	}
	if !row.ID.Valid || uuid.UUID(row.ID.Bytes) != artifactID ||
		!bytes.Equal(row.Sha256, digest) ||
		!bytes.Equal(row.Artifact, registration.Canonical) ||
		!jsonDocumentsEqual(row.ArtifactJson, registration.Canonical) {
		return errors.New("persisted adaptive evidence artifact is invalid")
	}
	return nil
}

func loadEvidenceArtifactTx(
	ctx context.Context,
	queries *sqlc.Queries,
	ownerID uuid.UUID,
	cohortID uuid.UUID,
	ref ChildArtifactRef,
) (EvidenceArtifactRegistration, error) {
	artifactID, err := uuid.Parse(ref.ArtifactID)
	if err != nil {
		return EvidenceArtifactRegistration{}, err
	}
	row, err := queries.GetAdaptiveEvidenceArtifact(
		ctx,
		sqlc.GetAdaptiveEvidenceArtifactParams{
			OwnerID: dbUUID(ownerID), CohortID: dbUUID(cohortID),
			ArtifactID: dbUUID(artifactID), Kind: ref.Kind,
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return EvidenceArtifactRegistration{}, errors.New(
			"adaptive evidence cohort artifact is missing",
		)
	}
	if err != nil {
		return EvidenceArtifactRegistration{}, err
	}
	if row.SchemaID != ref.ArtifactSchemaID ||
		row.Revision != int32(ref.ArtifactRevision) ||
		hex.EncodeToString(row.Sha256) != ref.ArtifactSHA256 ||
		!jsonDocumentsEqual(row.Artifact, row.ArtifactJson) {
		return EvidenceArtifactRegistration{}, errors.New(
			"persisted adaptive evidence cohort artifact is invalid",
		)
	}
	return EvidenceArtifactRegistration{
		Ref: ref, Canonical: bytes.Clone(row.Artifact),
	}, nil
}
