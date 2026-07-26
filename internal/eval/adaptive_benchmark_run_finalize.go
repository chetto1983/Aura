package eval

import (
	"errors"
	"slices"
)

// FinalizeAdaptiveBenchmarkReport binds verified runs to canonical evidence.
func FinalizeAdaptiveBenchmarkReport(
	report AdaptiveBenchmarkReport,
	scenarioRun AdaptiveBenchmarkScenarioRun,
	controlRun AdaptiveBenchmarkControlRun,
) (AdaptiveBenchmarkReport, error) {
	if !scenarioRun.verified || !controlRun.verified {
		return AdaptiveBenchmarkReport{}, errors.New(
			"adaptive benchmark report requires verified runs",
		)
	}
	if err := ValidateAdaptiveBenchmarkCostProvenance(
		scenarioRun,
		report.FrozenInputs,
	); err != nil {
		return AdaptiveBenchmarkReport{}, err
	}
	report.Scenarios = cloneAdaptiveBenchmarkScenarioRecords(
		scenarioRun.Scenarios,
	)
	report.Controls = cloneAdaptiveBenchmarkControlRecords(
		controlRun.Records,
	)
	report.Summary = BuildAdaptiveBenchmarkSummary(report.Scenarios)
	report.Verdict = ExpectedAdaptiveBenchmarkVerdict(report)
	if err := ValidateAdaptiveBenchmarkReport(report); err != nil {
		return AdaptiveBenchmarkReport{}, err
	}
	return report, nil
}

func cloneAdaptiveBenchmarkScenarioRecords(
	records []AdaptiveBenchmarkScenarioRecord,
) []AdaptiveBenchmarkScenarioRecord {
	cloned := make([]AdaptiveBenchmarkScenarioRecord, len(records))
	for index, record := range records {
		cloned[index] = record
		cloned[index].RequestID = cloneBenchmarkString(record.RequestID)
		cloned[index].AssignmentID = cloneBenchmarkString(record.AssignmentID)
		cloned[index].DeliveryEventID = cloneBenchmarkString(
			record.DeliveryEventID,
		)
		cloned[index].QualityOutcomeID = cloneBenchmarkString(
			record.QualityOutcomeID,
		)
		cloned[index].HarmOutcomeID = cloneBenchmarkString(
			record.HarmOutcomeID,
		)
		cloned[index].ActionProbabilities = slices.Clone(
			record.ActionProbabilities,
		)
		cloned[index].Quality = cloneAdaptiveBenchmarkObservation(record.Quality)
		cloned[index].Harm = cloneAdaptiveBenchmarkObservation(record.Harm)
		cloned[index].CostUSD = cloneBenchmarkFloat(record.CostUSD)
		cloned[index].ReasonCodes = slices.Clone(record.ReasonCodes)
	}
	return cloned
}

func cloneAdaptiveBenchmarkControlRecords(
	records []AdaptiveBenchmarkControlRecord,
) []AdaptiveBenchmarkControlRecord {
	cloned := slices.Clone(records)
	for index, record := range records {
		cloned[index].EvidenceIDs = slices.Clone(record.EvidenceIDs)
		cloned[index].ReasonCodes = slices.Clone(record.ReasonCodes)
	}
	return cloned
}

func cloneBenchmarkString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
