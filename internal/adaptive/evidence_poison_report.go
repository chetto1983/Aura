package adaptive

import (
	"encoding/json"
	"errors"
)

func buildPoisonGateReports(
	cohort *FocalCohort,
	look EvidenceLook,
	ledger CohortLedger,
) (gateReports, error) {
	if cohort == nil || !cohort.randomizationEligible() ||
		ledger.State != CohortLedgerClosed ||
		ledger.PoisonedMemberCount <= 0 || !validSHA256(ledger.SHA256) {
		return gateReports{}, errors.New(
			"adaptive poisoned gate report input is invalid",
		)
	}
	const reason = "poisoned_ledger"
	strataBytes, err := json.Marshal([]string{})
	if err != nil {
		return gateReports{}, err
	}
	strataSHA256 := sha256Hex(strataBytes)
	quality := newQualityATEReport(
		newATEReportPrefix(
			cohort, look, ledger.SHA256, strataSHA256, EvidenceEndpointQuality,
		),
		[]string{reason},
	)
	quality.Strata = []ATEReportStratum{}
	quality.MinimumUplift, err = exactRationalFromFloat(
		cohort.Margins().MinimumQualityUplift,
	)
	if err != nil {
		return gateReports{}, err
	}
	harm := newHarmATEReport(
		newATEReportPrefix(
			cohort, look, ledger.SHA256, strataSHA256, EvidenceEndpointHarm,
		),
		[]string{reason},
	)
	harm.Strata = []ATEReportStratum{}
	harm.MaximumIncrease, err = exactRationalFromFloat(
		cohort.Margins().MaximumHarmIncrease,
	)
	if err != nil {
		return gateReports{}, err
	}
	scope := cohort.Scope()
	censor := CensorDiagnosticsReport{
		SchemaID: "aura.adaptive.report/censor-diagnostics/v1", Revision: 1,
		ReportID: reportID(
			cohort.ID(), look.Number, "censor-diagnostics", ledger.SHA256,
		),
		OwnerID: scope.OwnerID.String(), CohortID: cohort.ID().String(),
		LookNumber: look.Number, LookCutoff: look.Cutoff,
		ClosedLedgerSHA256: ledger.SHA256,
		AnalysisStratumSchemaSHA256: cohort.v2.randomizationPlan.
			AnalysisStratumSchemaSHA256,
		TotalMembers: ledger.PoisonedMemberCount, Reasons: []string{reason},
	}
	qualityCanonical, err := quality.CanonicalJSON()
	if err != nil {
		return gateReports{}, err
	}
	harmCanonical, err := harm.CanonicalJSON()
	if err != nil {
		return gateReports{}, err
	}
	censorCanonical, err := censor.CanonicalJSON()
	if err != nil {
		return gateReports{}, err
	}
	return gateReports{
		Quality: quality, Harm: harm, Censor: censor,
		QualitySHA256: sha256Hex(qualityCanonical),
		HarmSHA256:    sha256Hex(harmCanonical),
		CensorSHA256:  sha256Hex(censorCanonical),
		StrataSHA256:  strataSHA256,
	}, nil
}

func buildPoisonOPEReports(
	cohort *FocalCohort,
	look EvidenceLook,
	ledger CohortLedger,
	children outcomeChildren,
) (opeReportPair, error) {
	if ledger.PoisonedMemberCount <= 0 {
		return opeReportPair{}, errors.New(
			"adaptive poisoned OPE requires raw members",
		)
	}
	var plan storedOPEPlanMetadata
	if err := decodeCanonicalRegistration(
		children.opePlan, "ope_plan", &plan,
	); err != nil {
		return opeReportPair{}, err
	}
	qualityRef, harmRef, err := endpointModelRefs(plan)
	if err != nil {
		return opeReportPair{}, err
	}
	var qualityModel, harmModel storedOutcomeModelMetadata
	if err := decodeCanonicalRegistration(
		children.qualityModel, "quality_outcome_model", &qualityModel,
	); err != nil {
		return opeReportPair{}, err
	}
	if err := decodeCanonicalRegistration(
		children.harmModel, "harm_outcome_model", &harmModel,
	); err != nil {
		return opeReportPair{}, err
	}
	if qualityRef.ArtifactID != children.qualityModel.Ref.ArtifactID ||
		qualityRef.ArtifactSHA256 != children.qualityModel.Ref.ArtifactSHA256 ||
		qualityRef.PredictionSHA256 != qualityModel.PredictionSHA256 ||
		harmRef.ArtifactID != children.harmModel.Ref.ArtifactID ||
		harmRef.ArtifactSHA256 != children.harmModel.Ref.ArtifactSHA256 ||
		harmRef.PredictionSHA256 != harmModel.PredictionSHA256 {
		return opeReportPair{}, errors.New(
			"adaptive poisoned OPE model binding is invalid",
		)
	}
	interference, err := DecodeInterferencePlanArtifact(
		children.interference.Canonical,
	)
	if err != nil {
		return opeReportPair{}, err
	}
	quality := newEndpointOPEReport(
		cohort, look, ledger, plan, interference.ClusterSchemaSHA256,
		EvidenceEndpointQuality, qualityRef, children.qualityModel,
	)
	harm := newEndpointOPEReport(
		cohort, look, ledger, plan, interference.ClusterSchemaSHA256,
		EvidenceEndpointHarm, harmRef, children.harmModel,
	)
	for _, report := range []*EndpointOPEReport{&quality, &harm} {
		report.RowCount = uint64(ledger.PoisonedMemberCount)
		markOPEReportIneligible(
			report, "poisoned_ledger", uint64(ledger.PoisonedMemberCount),
		)
	}
	return finalizeOPEReports(quality, harm)
}
