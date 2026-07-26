package adaptive

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var evidenceIDNamespace = uuid.MustParse("322f47a3-7362-469b-94fa-b7580a18a1dd")

func sealCanaryOutcomeTx(
	ctx context.Context,
	queries *sqlc.Queries,
	ownerID uuid.UUID,
	cohortID uuid.UUID,
	lookNumber int,
	sealer EvidenceSealer,
) (CanaryOutcomeEvidence, error) {
	if err := lockAdaptiveOwnerTx(ctx, queries, ownerID); err != nil {
		return CanaryOutcomeEvidence{}, err
	}
	if err := requireEvidenceSealerCapabilityTx(
		ctx, queries, sealer,
	); err != nil {
		return CanaryOutcomeEvidence{}, err
	}
	cohort, err := loadCohortForReconstructionTx(
		ctx, queries, ownerID, cohortID,
	)
	if err != nil {
		return CanaryOutcomeEvidence{}, err
	}
	if !cohort.randomizationEligible() {
		return CanaryOutcomeEvidence{}, ErrCohortV1RandomizationUnsupported
	}
	children, err := loadOutcomeChildrenTx(ctx, queries, cohort)
	if err != nil {
		return CanaryOutcomeEvidence{}, err
	}
	lookPlan, err := DecodeEvidenceLookPlanArtifact(children.lookPlan.Canonical)
	if err != nil || !lookPlan.Looks[4].CutoffAt.Equal(cohort.Cutoff()) {
		return CanaryOutcomeEvidence{}, errors.New(
			"stored adaptive look plan is invalid",
		)
	}
	lookArtifact := lookPlan.Looks[lookNumber-1]
	look := EvidenceLook{
		Number: lookArtifact.LookNumber, Cutoff: lookArtifact.CutoffAt,
		Alpha: lookArtifact.Alpha, CumulativeAlpha: lookArtifact.CumulativeAlpha,
		PredecessorRule: lookArtifact.PredecessorRule,
	}
	predecessor, err := loadOutcomePredecessorTx(
		ctx, queries, ownerID, cohortID, lookNumber,
	)
	if err != nil {
		return CanaryOutcomeEvidence{}, err
	}
	existing, err := queries.GetAdaptiveSealedOutcomeLook(
		ctx,
		sqlc.GetAdaptiveSealedOutcomeLookParams{
			OwnerID: dbUUID(ownerID), CohortID: dbUUID(cohortID),
			LookNumber: pgtype.Int4{Int32: int32(lookNumber), Valid: true},
		},
	)
	if err == nil {
		return outcomeEvidenceFromRow(existing, ownerID, cohortID, lookNumber)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return CanaryOutcomeEvidence{}, err
	}
	evidenceTime, err := queries.AdaptiveEvidenceTransactionTime(ctx)
	if err != nil {
		return CanaryOutcomeEvidence{}, fmt.Errorf(
			"read adaptive evidence time: %w", err,
		)
	}
	if !evidenceTime.Valid {
		return CanaryOutcomeEvidence{}, errors.New(
			"adaptive evidence time is unavailable",
		)
	}
	if evidenceTime.Time.Before(look.Cutoff) {
		return CanaryOutcomeEvidence{}, errors.New(
			"adaptive evidence look is open",
		)
	}
	ledger, err := reconstructCohortAtLookTx(
		ctx, queries, cohort, look.Cutoff, evidenceTime.Time,
	)
	poisoned := errors.Is(err, ErrInvalidCohortLedger)
	if err != nil && !poisoned {
		return CanaryOutcomeEvidence{}, err
	}
	memberCount := len(ledger.Members)
	if poisoned {
		memberCount = ledger.PoisonedMemberCount
	}
	if uint64(memberCount) < cohort.Power().MinimumMembers {
		return CanaryOutcomeEvidence{}, errors.New(
			"adaptive evidence look is below minimum members",
		)
	}
	var reports gateReports
	var opeReports opeReportPair
	if poisoned {
		reports, err = buildPoisonGateReports(cohort, look, ledger)
		if err == nil {
			opeReports, err = buildPoisonOPEReports(
				cohort, look, ledger, children,
			)
		}
	} else {
		reports, err = buildGateReports(ctx, cohort, look, ledger)
		if err == nil {
			opeReports, err = buildUnavailableOPEReports(
				cohort, look, ledger, children.qualityModel,
				children.harmModel, children.opePlan,
			)
		}
	}
	if err != nil {
		return CanaryOutcomeEvidence{}, err
	}
	gateValid := !poisoned && reports.GateValid &&
		realizedArmSupport(ledger.Members)
	qualityLower, harmUpper := MustExactRational(0, 1), MustExactRational(0, 1)
	if reports.Quality.ATELower != nil {
		qualityLower = *reports.Quality.ATELower
	}
	if reports.Harm.ATEUpper != nil {
		harmUpper = *reports.Harm.ATEUpper
	}
	decision := DecideOutcomeDisposition(DispositionInput{
		LookNumber: lookNumber, GateValid: gateValid,
		QualityLower:         qualityLower,
		MinimumQualityUplift: reports.Quality.MinimumUplift,
		HarmUpper:            harmUpper,
		MaximumHarmIncrease:  reports.Harm.MaximumIncrease,
	})
	if !decision.Write {
		return CanaryOutcomeEvidence{}, errors.New(
			"adaptive outcome disposition forbids a write",
		)
	}
	reasons := gateIneligibleReasons(reports, gateValid)
	body := CanaryOutcomeBody{
		SchemaID: "aura.adaptive.evidence/canary-outcome-body/v1", Revision: 1,
		TargetState: string(PolicyActive), LookNumber: lookNumber,
		LookCutoff: look.Cutoff, Alpha: look.Alpha,
		CumulativeAlpha:             look.CumulativeAlpha,
		RandomizationPlanSHA256:     cohort.v2.document.RandomizationPlanRef.ArtifactSHA256,
		AnalysisStratumSchemaSHA256: cohort.v2.randomizationPlan.AnalysisStratumSchemaSHA256,
		AnalysisStrataSHA256:        reports.StrataSHA256,
		ClosedLedgerSHA256:          ledger.SHA256,
		QualityReportID:             reports.Quality.ReportID,
		QualityReportSHA256:         reports.QualitySHA256,
		HarmReportID:                reports.Harm.ReportID, HarmReportSHA256: reports.HarmSHA256,
		QualityOPEReportID:      opeReports.Quality.ReportID,
		QualityOPEReportSHA256:  opeReports.QualitySHA256,
		HarmOPEReportID:         opeReports.Harm.ReportID,
		HarmOPEReportSHA256:     opeReports.HarmSHA256,
		CensorDiagnosticsID:     reports.Censor.ReportID,
		CensorDiagnosticsSHA256: reports.CensorSHA256,
		Disposition:             decision.Disposition, Eligible: decision.Eligible,
		IneligibleReasons: reasons,
	}
	if predecessor != nil {
		predecessorSHA256, _ := predecessor.SHA256()
		body.PredecessorEvidenceID = &predecessor.EvidenceID
		body.PredecessorEvidenceSHA256 = &predecessorSHA256
	}
	bodySHA256, err := body.SHA256()
	if err != nil {
		return CanaryOutcomeEvidence{}, err
	}
	scope := cohort.Scope()
	evidence := CanaryOutcomeEvidence{
		SchemaID: "aura.adaptive.evidence/canary-outcome/v1", Revision: 1,
		EvidenceID: sealedEvidenceID(
			cohortID, EvidenceCanaryOutcome, lookNumber, bodySHA256,
		),
		Kind: EvidenceCanaryOutcome, OwnerID: ownerID.String(),
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
	if err := storeOutcomeReportsTx(
		ctx, queries, ownerID, cohortID, sealer, reports, opeReports,
	); err != nil {
		return CanaryOutcomeEvidence{}, err
	}
	return persistOutcomeEvidenceTx(ctx, queries, evidence)
}

type outcomeChildren struct {
	lookPlan, interference, qualityModel, harmModel, opePlan EvidenceArtifactRegistration
}

func loadOutcomeChildrenTx(
	ctx context.Context,
	queries *sqlc.Queries,
	cohort *FocalCohort,
) (outcomeChildren, error) {
	document := cohort.v2.document
	refs := []*EvidenceArtifactRegistration{
		{}, {}, {}, {}, {},
	}
	bindings := []ChildArtifactRef{
		document.LookPlanRef, document.InterferencePlanRef,
		document.QualityOutcomeModelRef, document.HarmOutcomeModelRef,
		document.OPEPlanRef,
	}
	for index, binding := range bindings {
		loaded, err := loadEvidenceArtifactTx(
			ctx, queries, cohort.Scope().OwnerID, cohort.ID(), binding,
		)
		if err != nil {
			return outcomeChildren{}, err
		}
		*refs[index] = loaded
	}
	return outcomeChildren{
		lookPlan: *refs[0], interference: *refs[1],
		qualityModel: *refs[2], harmModel: *refs[3], opePlan: *refs[4],
	}, nil
}

func loadOutcomePredecessorTx(
	ctx context.Context,
	queries *sqlc.Queries,
	ownerID uuid.UUID,
	cohortID uuid.UUID,
	lookNumber int,
) (*CanaryOutcomeEvidence, error) {
	if lookNumber == 1 {
		return nil, nil
	}
	row, err := queries.GetAdaptiveSealedOutcomeLook(
		ctx,
		sqlc.GetAdaptiveSealedOutcomeLookParams{
			OwnerID: dbUUID(ownerID), CohortID: dbUUID(cohortID),
			LookNumber: pgtype.Int4{Int32: int32(lookNumber - 1), Valid: true},
		},
	)
	if err != nil {
		return nil, errors.New("adaptive outcome predecessor is missing")
	}
	predecessor, err := outcomeEvidenceFromRow(
		row, ownerID, cohortID, lookNumber-1,
	)
	if err != nil || predecessor.Body.Disposition != OutcomeDispositionContinue ||
		!predecessor.Body.Eligible {
		return nil, errors.New("adaptive outcome predecessor is invalid")
	}
	return &predecessor, nil
}

func requireEvidenceSealerCapabilityTx(
	ctx context.Context,
	queries *sqlc.Queries,
	sealer EvidenceSealer,
) error {
	allowed, err := queries.HasCapability(
		ctx,
		sqlc.HasCapabilityParams{
			IdentityID: dbUUID(sealer.ActorID), Capability: sealer.Capability,
		},
	)
	if err != nil {
		return err
	}
	if !allowed {
		return errors.New("adaptive evidence sealer capability is unavailable")
	}
	return nil
}
