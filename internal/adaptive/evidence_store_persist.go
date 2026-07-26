package adaptive

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func storeOutcomeReportsTx(
	ctx context.Context,
	queries *sqlc.Queries,
	ownerID uuid.UUID,
	cohortID uuid.UUID,
	sealer EvidenceSealer,
	reports gateReports,
	opeReports opeReportPair,
) error {
	gateArtifacts, err := gateReportArtifacts(reports)
	if err != nil {
		return err
	}
	opeArtifacts, err := opeReportArtifacts(opeReports)
	if err != nil {
		return err
	}
	for _, artifact := range append(gateArtifacts, opeArtifacts...) {
		if err := storeEvidenceArtifactTx(
			ctx, queries, ownerID, cohortID, sealer, artifact,
		); err != nil {
			return err
		}
	}
	return nil
}

func persistOutcomeEvidenceTx(
	ctx context.Context,
	queries *sqlc.Queries,
	evidence CanaryOutcomeEvidence,
) (CanaryOutcomeEvidence, error) {
	canonical, err := evidence.CanonicalJSON()
	if err != nil {
		return CanaryOutcomeEvidence{}, err
	}
	evidenceSHA256, err := evidence.SHA256()
	if err != nil {
		return CanaryOutcomeEvidence{}, err
	}
	evidenceID, _ := uuid.Parse(evidence.EvidenceID)
	ownerID, _ := uuid.Parse(evidence.OwnerID)
	cohortID, _ := uuid.Parse(evidence.CohortID)
	snapshotID, _ := uuid.Parse(evidence.SnapshotID)
	actorID, _ := uuid.Parse(evidence.SealerActorID)
	cohortSHA256, _ := hex.DecodeString(evidence.CohortSHA256)
	snapshotSHA256, _ := hex.DecodeString(evidence.SnapshotSHA256)
	bodySHA256, _ := hex.DecodeString(evidence.BodySHA256)
	evidenceDigest, _ := hex.DecodeString(evidenceSHA256)
	predecessorID, predecessorSHA256 := pgtype.UUID{}, []byte(nil)
	if evidence.Body.PredecessorEvidenceID != nil {
		id, _ := uuid.Parse(*evidence.Body.PredecessorEvidenceID)
		predecessorID = dbUUID(id)
		predecessorSHA256, _ = hex.DecodeString(
			*evidence.Body.PredecessorEvidenceSHA256,
		)
	}
	row, err := queries.SealAdaptiveEvidence(
		ctx,
		sqlc.SealAdaptiveEvidenceParams{
			ActorID: dbUUID(actorID), Kind: string(evidence.Kind),
			EvidenceID: dbUUID(evidenceID), OwnerID: dbUUID(ownerID),
			CohortID: dbUUID(cohortID), CohortSha256: cohortSHA256,
			Domain: string(evidence.Domain), ProviderID: evidence.ProviderID,
			ModelID: evidence.ModelID, PolicyEpoch: int64(evidence.PolicyEpoch),
			PolicyVersion: evidence.PolicyVersion, SnapshotID: dbUUID(snapshotID),
			SnapshotSha256: snapshotSHA256,
			LookNumber: pgtype.Int4{
				Int32: int32(evidence.Body.LookNumber), Valid: true,
			},
			LookCutoff:            dbTime(evidence.Body.LookCutoff),
			PredecessorEvidenceID: predecessorID,
			PredecessorSha256:     predecessorSHA256,
			Disposition:           dbText(string(evidence.Body.Disposition)),
			Eligible:              evidence.Body.Eligible, BodySha256: bodySHA256,
			EvidenceSha256: evidenceDigest, Artifact: canonical,
			ArtifactJson:     canonical,
			SealerCapability: evidence.SealerCapability,
		},
	)
	if err != nil {
		return CanaryOutcomeEvidence{}, fmt.Errorf(
			"persist adaptive canary outcome: %w", err,
		)
	}
	return outcomeEvidenceFromRow(
		row, ownerID, cohortID, evidence.Body.LookNumber,
	)
}

func outcomeEvidenceFromRow(
	row sqlc.AuraAdaptiveSealedEvidence,
	ownerID uuid.UUID,
	cohortID uuid.UUID,
	lookNumber int,
) (CanaryOutcomeEvidence, error) {
	evidence, err := DecodeCanaryOutcomeEvidence(row.Artifact)
	if err != nil {
		return CanaryOutcomeEvidence{}, err
	}
	evidenceSHA256, err := evidence.SHA256()
	if err != nil {
		return CanaryOutcomeEvidence{}, err
	}
	if !row.ID.Valid || uuid.UUID(row.ID.Bytes).String() != evidence.EvidenceID ||
		row.Kind != string(EvidenceCanaryOutcome) ||
		!row.OwnerID.Valid || uuid.UUID(row.OwnerID.Bytes) != ownerID ||
		!row.CohortID.Valid || uuid.UUID(row.CohortID.Bytes) != cohortID ||
		!row.LookNumber.Valid || int(row.LookNumber.Int32) != lookNumber ||
		hex.EncodeToString(row.Sha256) != evidenceSHA256 ||
		hex.EncodeToString(row.BodySha256) != evidence.BodySHA256 ||
		!jsonDocumentsEqual(row.Artifact, row.ArtifactJson) ||
		!row.CreatedAt.Valid ||
		!row.CreatedAt.Time.Equal(evidence.CreatedAt) {
		return CanaryOutcomeEvidence{}, errors.New(
			"persisted adaptive canary outcome is invalid",
		)
	}
	return evidence, nil
}

func sealedEvidenceID(
	cohortID uuid.UUID,
	kind EvidenceKind,
	lookNumber int,
	bodySHA256 string,
) string {
	preimage := []byte(
		string(kind) + "\x00" + cohortID.String() + "\x00" +
			strconv.Itoa(lookNumber) + "\x00" + bodySHA256,
	)
	return uuid.NewSHA1(evidenceIDNamespace, preimage).String()
}

func realizedArmSupport(members []CohortMember) bool {
	baseline, challenger := false, false
	for _, member := range members {
		baseline = baseline ||
			member.Assignment.ArmID == string(RandomizedArmBaseline)
		challenger = challenger ||
			member.Assignment.ArmID == string(RandomizedArmChallenger)
	}
	return baseline && challenger
}

func gateIneligibleReasons(
	reports gateReports,
	gateValid bool,
) []string {
	if gateValid {
		return []string{}
	}
	reasons := make([]string, 0, 6)
	reasons = append(reasons, reports.Quality.IneligibleReasons...)
	reasons = append(reasons, reports.Harm.IneligibleReasons...)
	reasons = append(reasons, reports.Censor.Reasons...)
	if !reports.GateValid && len(reasons) == 0 {
		reasons = append(reasons, "gate_support_failure")
	}
	slices.Sort(reasons)
	return slices.Compact(reasons)
}
