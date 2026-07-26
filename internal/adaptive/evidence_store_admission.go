package adaptive

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func sealCanaryAdmissionTx(
	ctx context.Context,
	queries *sqlc.Queries,
	ownerID uuid.UUID,
	cohortID uuid.UUID,
	sealer EvidenceSealer,
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
	document := cohort.v2.document
	refs := []ChildArtifactRef{
		document.InterferencePlanRef, document.LookPlanRef,
		document.QualityOutcomeModelRef, document.HarmOutcomeModelRef,
		document.OPEPlanRef,
	}
	for _, ref := range refs {
		if _, err := loadEvidenceArtifactTx(
			ctx, queries, ownerID, cohortID, ref,
		); err != nil {
			return CanaryAdmissionEvidence{}, err
		}
	}
	approvals, err := queries.ListAdaptiveEvidenceArtifactsByKind(
		ctx,
		sqlc.ListAdaptiveEvidenceArtifactsByKindParams{
			OwnerID: dbUUID(ownerID), CohortID: dbUUID(cohortID),
			Kind: "operator_approval",
		},
	)
	if err != nil || len(approvals) != 1 {
		return CanaryAdmissionEvidence{}, errors.New(
			"adaptive admission requires one stored operator approval",
		)
	}
	approval, err := DecodeOperatorApproval(approvals[0].Artifact)
	if err != nil || approval.OwnerID != ownerID.String() ||
		approval.CohortID != cohortID.String() ||
		approval.CohortSHA256 != cohort.SHA256() ||
		approval.PolicyEpoch != document.PolicyEpoch ||
		approval.PolicyVersion != document.PolicyVersion {
		return CanaryAdmissionEvidence{}, errors.New(
			"stored adaptive operator approval is invalid",
		)
	}
	approvalRef := ChildArtifactRef{
		SchemaID: ChildArtifactRefSchemaID, Revision: 1,
		Kind:             "operator_approval",
		ArtifactSchemaID: "aura.adaptive.operator-approval/v1",
		ArtifactRevision: 1, ArtifactID: approval.ApprovalID,
		ArtifactSHA256: hex.EncodeToString(approvals[0].Sha256),
	}
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
	existing, err := queries.GetAdaptiveSealedAdmission(
		ctx,
		sqlc.GetAdaptiveSealedAdmissionParams{
			OwnerID: dbUUID(ownerID), CohortID: dbUUID(cohortID),
		},
	)
	if err == nil {
		return admissionEvidenceFromRow(existing, ownerID, cohortID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
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
