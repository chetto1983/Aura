package adaptive

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/google/uuid"
)

func endpointCountsByStratum(
	members []CohortMember,
) ([]string, []BinaryATEStratum, []BinaryATEStratum, error) {
	if len(members) == 0 {
		return nil, nil, nil, errors.New("adaptive closed ledger has no members")
	}
	byStratum := make(map[string][]CohortMember)
	for _, member := range members {
		if !validSHA256(member.Claim.AnalysisStratumSchemaSHA256) ||
			!validSHA256(member.Claim.AnalysisStratumID) ||
			!validSHA256(member.Claim.InterferenceClusterSchemaSHA256) ||
			!validSHA256(member.Claim.InterferenceClusterID) ||
			!member.ITTEligible {
			return nil, nil, nil, errors.New(
				"adaptive closed member has invalid evidence identity",
			)
		}
		byStratum[member.Claim.AnalysisStratumID] = append(
			byStratum[member.Claim.AnalysisStratumID], member,
		)
	}
	ids := make([]string, 0, len(byStratum))
	for id := range byStratum {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	quality := make([]BinaryATEStratum, len(ids))
	harm := make([]BinaryATEStratum, len(ids))
	for index, id := range ids {
		members := byStratum[id]
		qualityMembers := make([]BinaryITTMember, len(members))
		harmMembers := make([]BinaryITTMember, len(members))
		for memberIndex, member := range members {
			treated := member.Assignment.ArmID == string(RandomizedArmChallenger)
			if !treated && member.Assignment.ArmID != string(RandomizedArmBaseline) {
				return nil, nil, nil, errors.New("adaptive member arm is invalid")
			}
			qualityMembers[memberIndex] = BinaryITTMember{
				Treated: treated,
				Outcome: binaryOutcome(member.Quality, true),
			}
			harmMembers[memberIndex] = BinaryITTMember{
				Treated: treated,
				Outcome: binaryOutcome(member.Harm, false),
			}
		}
		var err error
		quality[index], err = BuildEndpointCounts(
			id, EvidenceEndpointQuality, qualityMembers,
		)
		if err != nil {
			return nil, nil, nil, err
		}
		harm[index], err = BuildEndpointCounts(
			id, EvidenceEndpointHarm, harmMembers,
		)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	return ids, quality, harm, nil
}

func binaryOutcome(
	observation *CohortEffectiveObservation,
	quality bool,
) *bool {
	if observation == nil || observation.Status != OutcomeObserved {
		return nil
	}
	value := observation.Measures.Harm
	if quality {
		value = observation.Measures.Quality
	}
	if value == nil || (*value != 0 && *value != 1) {
		return nil
	}
	result := *value == 1
	return &result
}

func reportStrata(
	counts []BinaryATEStratum,
	exact ExactATEReport,
) []ATEReportStratum {
	byID := make(map[string]BinaryATEStratum, len(counts))
	for _, count := range counts {
		byID[count.AnalysisStratumID] = count
	}
	strata := make([]ATEReportStratum, len(exact.Strata))
	for index, bounds := range exact.Strata {
		count := byID[bounds.AnalysisStratumID]
		strata[index] = ATEReportStratum{
			SchemaID: "aura.adaptive.report/ate-stratum/v1", Revision: 1,
			AnalysisStratumID: bounds.AnalysisStratumID,
			N:                 count.Population, Treated: count.Treated,
			Control:          count.Population - count.Treated,
			TreatedSuccesses: count.TreatedSuccesses,
			ControlSuccesses: count.ControlSuccesses,
			L1:               bounds.Treatment.Lower, U1: bounds.Treatment.Upper,
			L0: bounds.Control.Lower, U0: bounds.Control.Upper,
		}
	}
	return strata
}

func buildCensorReport(
	cohort *FocalCohort,
	look EvidenceLook,
	ledger CohortLedger,
) (CensorDiagnosticsReport, error) {
	diagnostics := ledger.CensorDiagnostics
	if diagnostics.Members != len(ledger.Members) {
		return CensorDiagnosticsReport{}, errors.New(
			"adaptive censor diagnostics member count differs",
		)
	}
	var baseline, challenger CohortArmCensorDiagnostics
	for _, arm := range diagnostics.ArmDiagnostics {
		switch arm.ArmID {
		case string(RandomizedArmBaseline):
			baseline = arm
		case string(RandomizedArmChallenger):
			challenger = arm
		default:
			return CensorDiagnosticsReport{}, errors.New(
				"adaptive censor diagnostics arm is invalid",
			)
		}
	}
	rate := exactRate(diagnostics.Censored, diagnostics.Members)
	baselineRate := exactRate(baseline.Censored, baseline.Members)
	challengerRate := exactRate(challenger.Censored, challenger.Members)
	difference := challengerRate.Sub(baselineRate)
	if difference.Cmp(MustExactRational(0, 1)) < 0 {
		difference = baselineRate.Sub(challengerRate)
	}
	maxRate, err := exactRationalFromFloat(cohort.Censoring().MaxCensorRate)
	if err != nil {
		return CensorDiagnosticsReport{}, err
	}
	maxImbalance, err := exactRationalFromFloat(
		cohort.Censoring().MaxCensorImbalance,
	)
	if err != nil {
		return CensorDiagnosticsReport{}, err
	}
	totalPass := rate.Cmp(maxRate) <= 0
	differentialPass := difference.Cmp(maxImbalance) <= 0
	reasons := make([]string, 0, 2)
	if !differentialPass {
		reasons = append(reasons, "censor_imbalance_exceeded")
	}
	if !totalPass {
		reasons = append(reasons, "censor_rate_exceeded")
	}
	slices.Sort(reasons)
	schemaSHA256 := cohort.v2.randomizationPlan.AnalysisStratumSchemaSHA256
	report := CensorDiagnosticsReport{
		SchemaID: "aura.adaptive.report/censor-diagnostics/v1", Revision: 1,
		ReportID: reportID(
			cohort.ID(), look.Number, "censor-diagnostics", ledger.SHA256,
		),
		OwnerID: cohort.Scope().OwnerID.String(), CohortID: cohort.ID().String(),
		LookNumber: look.Number, LookCutoff: look.Cutoff,
		ClosedLedgerSHA256:          ledger.SHA256,
		AnalysisStratumSchemaSHA256: schemaSHA256,
		Computed:                    true, TotalMembers: diagnostics.Members,
		CensoredTotal: diagnostics.Censored, CensorRate: &rate,
		BaselineMembers: baseline.Members, BaselineCensored: baseline.Censored,
		BaselineRate: &baselineRate, ChallengerMembers: challenger.Members,
		ChallengerCensored: challenger.Censored,
		ChallengerRate:     &challengerRate, AbsoluteRateDifference: &difference,
		MaxCensorRate: &maxRate, MaxCensorImbalance: &maxImbalance,
		TotalPass: totalPass, DifferentialPass: differentialPass,
		Passes: totalPass && differentialPass, Reasons: reasons,
	}
	return report, nil
}

func exactRate(numerator, denominator int) ExactRational {
	if denominator <= 0 {
		return MustExactRational(0, 1)
	}
	return MustExactRational(int64(numerator), uint64(denominator))
}

func reportID(
	cohortID uuid.UUID,
	lookNumber int,
	kind string,
	ledgerSHA256 string,
) string {
	preimage, _ := json.Marshal([]string{
		cohortID.String(), strconv.Itoa(lookNumber), kind, ledgerSHA256,
	})
	return uuid.NewSHA1(evidenceReportNamespace, preimage).String()
}

func canonicalEvaluator(provenance EvaluatorProvenance) CanonicalEvaluator {
	return CanonicalEvaluator{
		Kind: string(provenance.Kind), ID: provenance.ID,
		Version: provenance.Version, RubricID: provenance.RubricID,
		CalibrationID:    provenance.CalibrationID,
		ProvenanceSHA256: provenance.ProvenanceSHA256,
	}
}

func gateReportArtifacts(
	reports gateReports,
) ([]EvidenceArtifactRegistration, error) {
	values := []struct {
		refKind, schemaID, id, sha256 string
		value                         interface{ CanonicalJSON() ([]byte, error) }
	}{
		{"quality_report", "aura.adaptive.report/quality-ate/v1", reports.Quality.ReportID, reports.QualitySHA256, reports.Quality},
		{"harm_report", "aura.adaptive.report/harm-ate/v1", reports.Harm.ReportID, reports.HarmSHA256, reports.Harm},
		{"censor_diagnostics", "aura.adaptive.report/censor-diagnostics/v1", reports.Censor.ReportID, reports.CensorSHA256, reports.Censor},
	}
	artifacts := make([]EvidenceArtifactRegistration, len(values))
	for index, value := range values {
		canonical, err := value.value.CanonicalJSON()
		if err != nil {
			return nil, fmt.Errorf("canonicalize %s: %w", value.refKind, err)
		}
		if sha256Hex(canonical) != value.sha256 {
			return nil, fmt.Errorf("%s hash changed", value.refKind)
		}
		artifacts[index] = EvidenceArtifactRegistration{
			Ref: ChildArtifactRef{
				SchemaID: ChildArtifactRefSchemaID, Revision: 1,
				Kind: value.refKind, ArtifactSchemaID: value.schemaID,
				ArtifactRevision: 1, ArtifactID: value.id,
				ArtifactSHA256: value.sha256,
			},
			Canonical: canonical,
		}
	}
	return artifacts, nil
}
