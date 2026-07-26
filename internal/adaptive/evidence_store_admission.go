package adaptive

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var admissionCohortArtifactKinds = []string{
	"interference_plan",
	"look_plan",
	"quality_outcome_model",
	"harm_outcome_model",
	"ope_plan",
}

// ErrCanaryAdmissionConflict rejects any non-identical seal after admission.
var ErrCanaryAdmissionConflict = errors.New(
	"adaptive canary admission conflicts with existing evidence",
)

func sealCanaryAdmissionTx(
	ctx context.Context,
	queries *sqlc.Queries,
	ownerID uuid.UUID,
	cohortID uuid.UUID,
	sealer EvidenceSealer,
	manifestSHA256 string,
	registrations []EvidenceArtifactRegistration,
	operation idempotency.Operation,
) (CanaryAdmissionEvidence, error) {
	if err := lockAdaptiveOwnerTx(ctx, queries, ownerID); err != nil {
		return CanaryAdmissionEvidence{}, err
	}
	if err := requireEvidenceSealerCapabilityTx(
		ctx, queries, sealer,
	); err != nil {
		return CanaryAdmissionEvidence{}, err
	}
	cohort, err := loadCohortForReconstructionTx(
		ctx, queries, ownerID, cohortID,
	)
	if err != nil {
		return CanaryAdmissionEvidence{}, err
	}
	if !cohort.randomizationEligible() {
		return CanaryAdmissionEvidence{}, ErrCohortV1RandomizationUnsupported
	}
	ordered, err := validateAdmissionRegistrations(cohort, registrations)
	if err != nil {
		return CanaryAdmissionEvidence{}, err
	}
	semanticFingerprint, err := AdmissionOperationFingerprint(
		manifestSHA256,
		ownerID,
		cohortID,
		ordered,
	)
	if err != nil {
		return CanaryAdmissionEvidence{}, err
	}
	operationIdentityID, err := uuid.Parse(operation.Key.IdentityID)
	if err != nil {
		return CanaryAdmissionEvidence{}, errors.New(
			"adaptive admission operation identity is invalid",
		)
	}
	operationRow, err := queries.LockOperationReceipt(
		ctx,
		sqlc.LockOperationReceiptParams{
			IdentityID:     dbUUID(operationIdentityID),
			OperationScope: string(operation.Key.Scope),
			OperationKey:   operation.Key.Key,
		},
	)
	if err != nil {
		return CanaryAdmissionEvidence{}, errors.New(
			"adaptive admission operation receipt is unavailable",
		)
	}
	receipt, err := validateAdmissionOperationReceipt(
		operation,
		operationRow,
		semanticFingerprint,
	)
	if err != nil {
		return CanaryAdmissionEvidence{}, err
	}
	approvalRegistration, err := buildOperatorApprovalRegistration(
		cohort,
		sealer,
		ordered,
		receipt,
	)
	if err != nil {
		return CanaryAdmissionEvidence{}, err
	}
	existing, err := existingCanaryAdmissionRetryTx(
		ctx,
		queries,
		ownerID,
		cohortID,
		cohort,
		sealer,
		approvalRegistration,
	)
	if err != nil {
		return CanaryAdmissionEvidence{}, err
	}
	if existing != nil {
		return *existing, nil
	}
	for _, registration := range ordered {
		if err := storeEvidenceArtifactTx(
			ctx,
			queries,
			ownerID,
			cohortID,
			sealer,
			registration,
		); err != nil {
			return CanaryAdmissionEvidence{}, err
		}
	}
	if err := storeEvidenceArtifactTx(
		ctx,
		queries,
		ownerID,
		cohortID,
		sealer,
		approvalRegistration,
	); err != nil {
		return CanaryAdmissionEvidence{}, err
	}
	storedApproval, err := loadEvidenceArtifactTx(
		ctx,
		queries,
		ownerID,
		cohortID,
		approvalRegistration.Ref,
	)
	if err != nil {
		return CanaryAdmissionEvidence{}, err
	}
	if _, err := validateOperatorApprovalRegistration(
		cohort,
		sealer,
		storedApproval,
	); err != nil {
		return CanaryAdmissionEvidence{}, err
	}
	document := cohort.v2.document
	approvalRef := approvalRegistration.Ref
	randomizationCanonical, err := cohort.canonicalRandomizationPlanArtifact()
	if err != nil {
		return CanaryAdmissionEvidence{}, err
	}
	if err := storeEvidenceArtifactTx(
		ctx, queries, ownerID, cohortID, sealer,
		EvidenceArtifactRegistration{
			Ref: document.RandomizationPlanRef, Canonical: randomizationCanonical,
		},
	); err != nil {
		return CanaryAdmissionEvidence{}, err
	}
	evaluators, err := json.Marshal(document.Evaluators)
	if err != nil {
		return CanaryAdmissionEvidence{}, err
	}
	body := CanaryAdmissionBody{
		SchemaID: "aura.adaptive.evidence/canary-admission-body/v1", Revision: 1,
		TargetState:          string(PolicyCanary),
		RandomizationPlanRef: document.RandomizationPlanRef,
		InterferencePlanRef:  document.InterferencePlanRef,
		LookPlanRef:          document.LookPlanRef,
		SnapshotRef: ChildArtifactRef{
			SchemaID: ChildArtifactRefSchemaID, Revision: 1, Kind: "snapshot",
			ArtifactSchemaID: SnapshotSchemaVersion, ArtifactRevision: 1,
			ArtifactID:     document.SnapshotID,
			ArtifactSHA256: document.SnapshotSHA256,
		},
		EvaluatorCalibrationSHA256: sha256Hex(evaluators),
		QualityOutcomeModelRef:     document.QualityOutcomeModelRef,
		HarmOutcomeModelRef:        document.HarmOutcomeModelRef,
		OPEPlanRef:                 document.OPEPlanRef,
		PowerSimulationSHA256:      document.Power.SimulationSHA256,
		OperatorApprovalRef:        approvalRef,
	}
	bodySHA256, err := body.SHA256()
	if err != nil {
		return CanaryAdmissionEvidence{}, err
	}
	evidenceTime, err := queries.AdaptiveEvidenceTransactionTime(ctx)
	if err != nil {
		return CanaryAdmissionEvidence{}, fmt.Errorf(
			"read adaptive evidence time: %w", err,
		)
	}
	if !evidenceTime.Valid {
		return CanaryAdmissionEvidence{}, errors.New(
			"adaptive evidence time is unavailable",
		)
	}
	scope := cohort.Scope()
	evidence := CanaryAdmissionEvidence{
		SchemaID: "aura.adaptive.evidence/canary-admission/v1", Revision: 1,
		EvidenceID: sealedEvidenceID(
			cohortID, EvidenceCanaryAdmission, 0, bodySHA256,
		),
		Kind: EvidenceCanaryAdmission, OwnerID: ownerID.String(),
		CohortID: cohortID.String(), CohortSHA256: cohort.SHA256(),
		Domain: cohort.Domain(), ProviderID: scope.ProviderID, ModelID: scope.ModelID,
		PolicyEpoch: scope.PolicyEpoch, PolicyVersion: scope.PolicyVersion,
		SnapshotID:       scope.SnapshotID.String(),
		SnapshotSHA256:   scope.SnapshotSHA256,
		CreatedAt:        evidenceTime.Time.UTC().Truncate(time.Microsecond),
		SealerActorID:    sealer.ActorID.String(),
		SealerCapability: sealer.Capability,
		BodySHA256:       bodySHA256, Body: body,
	}
	return persistAdmissionEvidenceTx(ctx, queries, evidence)
}

func existingCanaryAdmissionRetryTx(
	ctx context.Context,
	queries *sqlc.Queries,
	ownerID uuid.UUID,
	cohortID uuid.UUID,
	cohort *FocalCohort,
	sealer EvidenceSealer,
	expectedApproval EvidenceArtifactRegistration,
) (*CanaryAdmissionEvidence, error) {
	row, err := queries.GetAdaptiveSealedAdmission(
		ctx,
		sqlc.GetAdaptiveSealedAdmissionParams{
			OwnerID:  dbUUID(ownerID),
			CohortID: dbUUID(cohortID),
		},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	existing, err := admissionEvidenceFromRow(row, ownerID, cohortID)
	if err != nil ||
		existing.Body.OperatorApprovalRef != expectedApproval.Ref {
		return nil, ErrCanaryAdmissionConflict
	}
	storedApproval, err := loadEvidenceArtifactTx(
		ctx,
		queries,
		ownerID,
		cohortID,
		existing.Body.OperatorApprovalRef,
	)
	if err != nil {
		return nil, ErrCanaryAdmissionConflict
	}
	if _, err := validateOperatorApprovalRegistration(
		cohort,
		sealer,
		storedApproval,
	); err != nil ||
		!bytes.Equal(storedApproval.Canonical, expectedApproval.Canonical) {
		return nil, ErrCanaryAdmissionConflict
	}
	return &existing, nil
}

func validateAdmissionRegistrations(
	cohort *FocalCohort,
	registrations []EvidenceArtifactRegistration,
) ([]EvidenceArtifactRegistration, error) {
	if len(registrations) != len(admissionCohortArtifactKinds) {
		return nil, errors.New(
			"adaptive admission requires five cohort artifacts",
		)
	}
	ordered := make(
		[]EvidenceArtifactRegistration,
		len(admissionCohortArtifactKinds),
	)
	for index, registration := range registrations {
		if registration.Ref.Kind != admissionCohortArtifactKinds[index] {
			return nil, errors.New(
				"adaptive admission cohort artifact order is invalid",
			)
		}
		if err := registration.validate(
			admissionCohortArtifactKinds...,
		); err != nil {
			return nil, err
		}
		if err := validateAdmissionCohortArtifact(
			cohort,
			registration,
		); err != nil {
			return nil, err
		}
		ordered[index] = registration
	}
	return ordered, nil
}

func persistAdmissionEvidenceTx(
	ctx context.Context,
	queries *sqlc.Queries,
	evidence CanaryAdmissionEvidence,
) (CanaryAdmissionEvidence, error) {
	canonical, err := evidence.CanonicalJSON()
	if err != nil {
		return CanaryAdmissionEvidence{}, err
	}
	evidenceSHA256, _ := evidence.SHA256()
	evidenceID, _ := uuid.Parse(evidence.EvidenceID)
	ownerID, _ := uuid.Parse(evidence.OwnerID)
	cohortID, _ := uuid.Parse(evidence.CohortID)
	snapshotID, _ := uuid.Parse(evidence.SnapshotID)
	actorID, _ := uuid.Parse(evidence.SealerActorID)
	cohortSHA256, _ := hex.DecodeString(evidence.CohortSHA256)
	snapshotSHA256, _ := hex.DecodeString(evidence.SnapshotSHA256)
	bodySHA256, _ := hex.DecodeString(evidence.BodySHA256)
	evidenceDigest, _ := hex.DecodeString(evidenceSHA256)
	row, err := queries.SealAdaptiveEvidence(
		ctx,
		sqlc.SealAdaptiveEvidenceParams{
			ActorID: dbUUID(actorID), Kind: string(evidence.Kind),
			EvidenceID: dbUUID(evidenceID), OwnerID: dbUUID(ownerID),
			CohortID: dbUUID(cohortID), CohortSha256: cohortSHA256,
			Domain: string(evidence.Domain), ProviderID: evidence.ProviderID,
			ModelID: evidence.ModelID, PolicyEpoch: int64(evidence.PolicyEpoch),
			PolicyVersion: evidence.PolicyVersion, SnapshotID: dbUUID(snapshotID),
			SnapshotSha256: snapshotSHA256, LookNumber: pgtype.Int4{},
			LookCutoff:            pgtype.Timestamptz{},
			PredecessorEvidenceID: pgtype.UUID{}, PredecessorSha256: nil,
			Disposition: pgtype.Text{}, Eligible: true, BodySha256: bodySHA256,
			EvidenceSha256: evidenceDigest, Artifact: canonical,
			ArtifactJson:     canonical,
			SealerCapability: evidence.SealerCapability,
		},
	)
	if err != nil {
		return CanaryAdmissionEvidence{}, err
	}
	return admissionEvidenceFromRow(row, ownerID, cohortID)
}

func admissionEvidenceFromRow(
	row sqlc.AuraAdaptiveSealedEvidence,
	ownerID uuid.UUID,
	cohortID uuid.UUID,
) (CanaryAdmissionEvidence, error) {
	evidence, err := DecodeCanaryAdmissionEvidence(row.Artifact)
	if err != nil {
		return CanaryAdmissionEvidence{}, err
	}
	evidenceSHA256, err := evidence.SHA256()
	if err != nil {
		return CanaryAdmissionEvidence{}, err
	}
	if !row.ID.Valid || uuid.UUID(row.ID.Bytes).String() != evidence.EvidenceID ||
		row.Kind != string(EvidenceCanaryAdmission) ||
		!row.OwnerID.Valid || uuid.UUID(row.OwnerID.Bytes) != ownerID ||
		!row.CohortID.Valid || uuid.UUID(row.CohortID.Bytes) != cohortID ||
		hex.EncodeToString(row.Sha256) != evidenceSHA256 ||
		hex.EncodeToString(row.BodySha256) != evidence.BodySHA256 ||
		!jsonDocumentsEqual(row.Artifact, row.ArtifactJson) ||
		!row.CreatedAt.Valid || !row.CreatedAt.Time.Equal(evidence.CreatedAt) {
		return CanaryAdmissionEvidence{}, errors.New(
			"persisted adaptive canary admission is invalid",
		)
	}
	return evidence, nil
}
