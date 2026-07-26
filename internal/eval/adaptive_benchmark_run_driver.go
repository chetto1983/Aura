package eval

import (
	"context"
	"errors"
	"math"
	"slices"

	"github.com/chetto1983/aura/internal/adaptive"
)

// ErrAdaptiveBenchmarkUnsafeDispatch stops a run before another side effect.
var ErrAdaptiveBenchmarkUnsafeDispatch = errors.New(
	"adaptive benchmark unsafe dispatch",
)

// AdaptiveBenchmarkCostBasis records how a nullable scenario cost was obtained.
type AdaptiveBenchmarkCostBasis string

// Cost bases preserve missingness and price-table provenance outside the report.
const (
	AdaptiveBenchmarkCostUnknown          AdaptiveBenchmarkCostBasis = "unknown"
	AdaptiveBenchmarkCostProviderReported AdaptiveBenchmarkCostBasis = "provider_reported"
	AdaptiveBenchmarkCostPriceTable       AdaptiveBenchmarkCostBasis = "price_table"
)

// AdaptiveBenchmarkPriceTable binds calculated cost to one exact table.
type AdaptiveBenchmarkPriceTable struct {
	ID       string
	Revision int
	SHA256   string
}

// AdaptiveBenchmarkDriverResult contains real Runner.Turn facts and transient output.
type AdaptiveBenchmarkDriverResult struct {
	RequestID           string
	AssignmentID        string
	DeliveryEventID     string
	HarmOutcomeID       string
	ChampionActionID    string
	RecommendedActionID string
	IntendedActionID    string
	ActualActionID      string
	ActionProbabilities []AdaptiveBenchmarkActionProbability
	Harm                *AdaptiveBenchmarkObservation
	PromptTokens        int64
	CompletionTokens    int64
	TotalTokens         int64
	LatencyNS           int64
	CostUSD             *float64
	CostBasis           AdaptiveBenchmarkCostBasis
	PriceTable          *AdaptiveBenchmarkPriceTable
	FailureReasonCodes  []string
	Observed            AdaptiveBenchmarkObserved
}

// AdaptiveBenchmarkSubject is the content-only input visible to the subject.
type AdaptiveBenchmarkSubject struct {
	ScenarioID string
	Domain     adaptive.Domain
	Prompt     string
}

// AdaptiveBenchmarkEvaluationRecord carries no gold, only checker disposition.
type AdaptiveBenchmarkEvaluationRecord struct {
	ScenarioID      string
	RequestID       string
	AssignmentID    string
	DeliveryEventID string
	Evaluation      AdaptiveBenchmarkEvaluation
}

// AdaptiveBenchmarkRecordedEvaluation is persisted outcome linkage.
type AdaptiveBenchmarkRecordedEvaluation struct {
	QualityOutcomeID string
	Quality          *AdaptiveBenchmarkObservation
}

// AdaptiveBenchmarkDriver is implemented by cmd/aura over the real Runner.Turn path.
type AdaptiveBenchmarkDriver interface {
	RunScenario(
		context.Context,
		AdaptiveBenchmarkSubject,
	) (AdaptiveBenchmarkDriverResult, error)
	RecordEvaluation(
		context.Context,
		AdaptiveBenchmarkEvaluationRecord,
	) (AdaptiveBenchmarkRecordedEvaluation, error)
}

// AdaptiveBenchmarkScenarioRun is content-free output plus cost provenance counts.
type AdaptiveBenchmarkScenarioRun struct {
	Scenarios           []AdaptiveBenchmarkScenarioRecord
	ProviderCostCount   int
	PriceTableCostCount int
	MissingCostCount    int
	PriceTable          *AdaptiveBenchmarkPriceTable
	verified            bool
}

// RunAdaptiveBenchmarkScenarios executes one real Aura turn at a time.
func RunAdaptiveBenchmarkScenarios(
	ctx context.Context,
	dataset AdaptiveBenchmarkDataset,
	driver AdaptiveBenchmarkDriver,
) (AdaptiveBenchmarkScenarioRun, error) {
	if driver == nil {
		return AdaptiveBenchmarkScenarioRun{}, errors.New(
			"adaptive benchmark driver is unavailable",
		)
	}
	if err := validateAdaptiveBenchmarkDataset(dataset); err != nil {
		return AdaptiveBenchmarkScenarioRun{}, err
	}
	ordered := AdaptiveBenchmarkScenarioOrder(dataset.Scenarios)
	run := AdaptiveBenchmarkScenarioRun{
		Scenarios: make(
			[]AdaptiveBenchmarkScenarioRecord,
			0,
			len(ordered),
		),
	}
	for _, scenario := range ordered {
		if err := ctx.Err(); err != nil {
			return AdaptiveBenchmarkScenarioRun{}, err
		}
		result, err := driver.RunScenario(ctx, AdaptiveBenchmarkSubject{
			ScenarioID: scenario.ScenarioID,
			Domain:     scenario.Domain,
			Prompt:     scenario.Prompt,
		})
		if ctxErr := ctx.Err(); ctxErr != nil {
			return AdaptiveBenchmarkScenarioRun{}, ctxErr
		}
		if errors.Is(err, ErrAdaptiveBenchmarkUnsafeDispatch) {
			return AdaptiveBenchmarkScenarioRun{}, err
		}
		var evaluation AdaptiveBenchmarkEvaluation
		var recorded AdaptiveBenchmarkRecordedEvaluation
		if err == nil && len(result.FailureReasonCodes) == 0 {
			var evaluationErr error
			evaluation, evaluationErr = EvaluateAdaptiveBenchmarkScenario(
				scenario,
				result.Observed,
			)
			switch {
			case evaluationErr != nil:
				result.FailureReasonCodes = []string{
					AdaptiveBenchmarkReasonEvaluatorMismatch,
				}
				err = evaluationErr
			case !validAdaptiveBenchmarkEvaluationLinkage(result):
				result.FailureReasonCodes = []string{
					AdaptiveBenchmarkReasonFactLinkageInvalid,
				}
				err = errors.New(
					"adaptive benchmark evaluation linkage is invalid",
				)
			default:
				recorded, err = driver.RecordEvaluation(
					ctx,
					AdaptiveBenchmarkEvaluationRecord{
						ScenarioID:      scenario.ScenarioID,
						RequestID:       result.RequestID,
						AssignmentID:    result.AssignmentID,
						DeliveryEventID: result.DeliveryEventID,
						Evaluation:      evaluation,
					},
				)
				if err != nil {
					result.FailureReasonCodes = []string{
						AdaptiveBenchmarkReasonOutcomeMissing,
					}
				}
			}
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return AdaptiveBenchmarkScenarioRun{}, ctxErr
		}
		record := adaptiveBenchmarkRecordFromDriver(
			scenario,
			result,
			evaluation,
			recorded,
			err,
		)
		if record.CostUSD != nil &&
			result.CostBasis == AdaptiveBenchmarkCostPriceTable {
			if run.PriceTable == nil {
				run.PriceTable = cloneAdaptiveBenchmarkPriceTable(
					result.PriceTable,
				)
			} else if result.PriceTable == nil ||
				*run.PriceTable != *result.PriceTable {
				record = invalidAdaptiveBenchmarkDriverRecord(
					record,
					AdaptiveBenchmarkReasonProvenanceMissing,
				)
				record.CostUSD = nil
			}
		}
		switch {
		case record.CostUSD == nil:
			run.MissingCostCount++
		case result.CostBasis == AdaptiveBenchmarkCostProviderReported:
			run.ProviderCostCount++
		case result.CostBasis == AdaptiveBenchmarkCostPriceTable:
			run.PriceTableCostCount++
		default:
			run.MissingCostCount++
		}
		run.Scenarios = append(run.Scenarios, record)
	}
	if err := validateAdaptiveBenchmarkScenarios(run.Scenarios); err != nil {
		return AdaptiveBenchmarkScenarioRun{}, err
	}
	run.verified = true
	return run, nil
}

func adaptiveBenchmarkRecordFromDriver(
	scenario AdaptiveBenchmarkScenario,
	result AdaptiveBenchmarkDriverResult,
	evaluation AdaptiveBenchmarkEvaluation,
	recorded AdaptiveBenchmarkRecordedEvaluation,
	driverErr error,
) AdaptiveBenchmarkScenarioRecord {
	probabilities := slices.Clone(result.ActionProbabilities)
	if probabilities == nil {
		probabilities = []AdaptiveBenchmarkActionProbability{}
	}
	record := AdaptiveBenchmarkScenarioRecord{
		ScenarioID:          scenario.ScenarioID,
		Domain:              scenario.Domain,
		RequestID:           benchmarkStringPointer(result.RequestID),
		AssignmentID:        benchmarkStringPointer(result.AssignmentID),
		DeliveryEventID:     benchmarkStringPointer(result.DeliveryEventID),
		QualityOutcomeID:    benchmarkStringPointer(recorded.QualityOutcomeID),
		HarmOutcomeID:       benchmarkStringPointer(result.HarmOutcomeID),
		ChampionActionID:    result.ChampionActionID,
		RecommendedActionID: result.RecommendedActionID,
		IntendedActionID:    result.IntendedActionID,
		ActualActionID:      result.ActualActionID,
		ActionProbabilities: probabilities,
		Quality:             cloneAdaptiveBenchmarkObservation(recorded.Quality),
		Harm:                cloneAdaptiveBenchmarkObservation(result.Harm),
		PromptTokens:        result.PromptTokens,
		CompletionTokens:    result.CompletionTokens,
		TotalTokens:         result.TotalTokens,
		LatencyNS:           result.LatencyNS,
		CostUSD:             cloneBenchmarkFloat(result.CostUSD),
		ReasonCodes:         []string{},
	}
	if driverErr != nil || len(result.FailureReasonCodes) != 0 {
		reasons := slices.Clone(result.FailureReasonCodes)
		if len(reasons) == 0 || !validAdaptiveBenchmarkReasonCodes(reasons) {
			reasons = []string{AdaptiveBenchmarkReasonDriverFailure}
		}
		record.Status = adaptiveBenchmarkFailureStatus(reasons)
		record.ReasonCodes = reasons
		if record.Status == AdaptiveBenchmarkStatusFail &&
			(!validBenchmarkStringUUID(record.RequestID) ||
				!validBenchmarkStringUUID(record.AssignmentID) ||
				!validBenchmarkStringUUID(record.DeliveryEventID)) {
			return invalidAdaptiveBenchmarkDriverRecord(
				record,
				AdaptiveBenchmarkReasonFactLinkageInvalid,
			)
		}
		return sanitizeAdaptiveBenchmarkFailureRecord(record)
	}
	if err := validateAdaptiveBenchmarkDriverEvaluation(
		evaluation,
		recorded.Quality,
	); err != nil {
		return invalidAdaptiveBenchmarkDriverRecord(
			record,
			AdaptiveBenchmarkReasonEvaluatorMismatch,
		)
	}
	if err := validateAdaptiveBenchmarkDriverCost(result); err != nil {
		return invalidAdaptiveBenchmarkDriverRecord(
			record,
			AdaptiveBenchmarkReasonProvenanceMissing,
		)
	}
	record.Status = AdaptiveBenchmarkStatusFail
	if evaluation.Passed {
		record.Status = AdaptiveBenchmarkStatusPass
	}
	record.ReasonCodes = slices.Clone(evaluation.ReasonCodes)
	if err := validateAdaptiveBenchmarkScenario(record); err != nil {
		return invalidAdaptiveBenchmarkDriverRecord(
			record,
			adaptiveBenchmarkDriverRecordReason(record),
		)
	}
	return record
}

func validAdaptiveBenchmarkEvaluationLinkage(
	result AdaptiveBenchmarkDriverResult,
) bool {
	return validBenchmarkUUID(result.RequestID) &&
		validBenchmarkUUID(result.AssignmentID) &&
		validBenchmarkUUID(result.DeliveryEventID)
}

func validateAdaptiveBenchmarkDriverEvaluation(
	evaluation AdaptiveBenchmarkEvaluation,
	observation *AdaptiveBenchmarkObservation,
) error {
	wantValue := float64(0)
	if evaluation.Passed {
		wantValue = 1
	}
	if observation == nil ||
		observation.EvaluatorID != AdaptiveBenchmarkEvaluatorID ||
		observation.EvaluatorRevision != AdaptiveBenchmarkEvaluatorRevision ||
		!observation.Eligible ||
		observation.Value == nil ||
		*observation.Value != wantValue ||
		!slices.Equal(observation.ReasonCodes, evaluation.ReasonCodes) {
		return errors.New(
			"driver quality outcome does not match deterministic evaluation",
		)
	}
	return nil
}

func adaptiveBenchmarkFailureStatus(
	reasons []string,
) AdaptiveBenchmarkStatus {
	for _, reason := range reasons {
		if reason != AdaptiveBenchmarkReasonModelParserFailure &&
			reason != AdaptiveBenchmarkReasonModelTransportFailure {
			return AdaptiveBenchmarkStatusInvalid
		}
	}
	return AdaptiveBenchmarkStatusFail
}

func invalidAdaptiveBenchmarkDriverRecord(
	record AdaptiveBenchmarkScenarioRecord,
	reason string,
) AdaptiveBenchmarkScenarioRecord {
	record.Status = AdaptiveBenchmarkStatusInvalid
	record.ReasonCodes = []string{reason}
	return sanitizeAdaptiveBenchmarkFailureRecord(record)
}

func sanitizeAdaptiveBenchmarkFailureRecord(
	record AdaptiveBenchmarkScenarioRecord,
) AdaptiveBenchmarkScenarioRecord {
	for _, id := range []**string{
		&record.RequestID,
		&record.AssignmentID,
		&record.DeliveryEventID,
		&record.QualityOutcomeID,
		&record.HarmOutcomeID,
	} {
		if *id != nil && !validBenchmarkUUID(**id) {
			*id = nil
		}
	}
	if (record.Quality == nil) != (record.QualityOutcomeID == nil) ||
		validateAdaptiveBenchmarkObservation(record.Quality, true) != nil ||
		(record.Status == AdaptiveBenchmarkStatusInvalid &&
			record.Quality != nil &&
			!slices.Equal(
				record.Quality.ReasonCodes,
				record.ReasonCodes,
			)) {
		record.Quality = nil
		record.QualityOutcomeID = nil
	}
	if (record.Harm == nil) != (record.HarmOutcomeID == nil) ||
		validateAdaptiveBenchmarkObservation(record.Harm, false) != nil {
		record.Harm = nil
		record.HarmOutcomeID = nil
	}
	if validateAdaptiveBenchmarkActions(record) != nil {
		record.ChampionActionID = ""
		record.RecommendedActionID = ""
		record.IntendedActionID = ""
		record.ActualActionID = ""
		record.ActionProbabilities = []AdaptiveBenchmarkActionProbability{}
	}
	if record.PromptTokens < 0 ||
		record.CompletionTokens < 0 ||
		record.PromptTokens > math.MaxInt64-record.CompletionTokens ||
		record.TotalTokens != record.PromptTokens+record.CompletionTokens {
		record.PromptTokens = 0
		record.CompletionTokens = 0
		record.TotalTokens = 0
	}
	if record.LatencyNS < 0 {
		record.LatencyNS = 0
	}
	if record.CostUSD != nil &&
		(math.IsNaN(*record.CostUSD) ||
			math.IsInf(*record.CostUSD, 0) ||
			*record.CostUSD < 0 ||
			math.Signbit(*record.CostUSD)) {
		record.CostUSD = nil
	}
	if !validAdaptiveBenchmarkReasonCodes(record.ReasonCodes) {
		record.ReasonCodes = []string{AdaptiveBenchmarkReasonDriverFailure}
	}
	return record
}

func adaptiveBenchmarkDriverRecordReason(
	record AdaptiveBenchmarkScenarioRecord,
) string {
	if record.RequestID == nil ||
		record.AssignmentID == nil ||
		record.DeliveryEventID == nil ||
		record.QualityOutcomeID == nil {
		return AdaptiveBenchmarkReasonFactLinkageInvalid
	}
	if validateAdaptiveBenchmarkActions(record) != nil {
		return AdaptiveBenchmarkReasonControlFailed
	}
	if record.Quality == nil ||
		validateAdaptiveBenchmarkObservation(record.Quality, true) != nil {
		return AdaptiveBenchmarkReasonEvaluatorMismatch
	}
	return AdaptiveBenchmarkReasonProvenanceMissing
}

func validateAdaptiveBenchmarkDriverCost(
	result AdaptiveBenchmarkDriverResult,
) error {
	switch result.CostBasis {
	case AdaptiveBenchmarkCostUnknown:
		if result.CostUSD != nil || result.PriceTable != nil {
			return errors.New(
				"unknown benchmark cost or price table must be null",
			)
		}
	case AdaptiveBenchmarkCostProviderReported:
		if result.CostUSD == nil || result.PriceTable != nil {
			return errors.New(
				"provider benchmark cost provenance is invalid",
			)
		}
	case AdaptiveBenchmarkCostPriceTable:
		if result.CostUSD == nil ||
			!validAdaptiveBenchmarkPriceTable(result.PriceTable) {
			return errors.New("known benchmark cost is missing")
		}
	default:
		return errors.New("benchmark cost basis is invalid")
	}
	return nil
}

// ValidateAdaptiveBenchmarkCostProvenance binds transient bases to report input.
func ValidateAdaptiveBenchmarkCostProvenance(
	run AdaptiveBenchmarkScenarioRun,
	inputs AdaptiveBenchmarkFrozenInputs,
) error {
	if !run.verified {
		return errors.New("adaptive benchmark scenario run is unverified")
	}
	if len(run.Scenarios) !=
		run.ProviderCostCount+run.PriceTableCostCount+run.MissingCostCount {
		return errors.New("adaptive benchmark cost counts are inconsistent")
	}
	costCount := 0
	for _, scenario := range run.Scenarios {
		if scenario.CostUSD != nil {
			costCount++
		}
	}
	if costCount != run.ProviderCostCount+run.PriceTableCostCount ||
		run.ProviderCostCount < 0 ||
		run.PriceTableCostCount < 0 ||
		run.MissingCostCount < 0 {
		return errors.New("adaptive benchmark cost bases are inconsistent")
	}
	priceFields := inputs.PriceTableID != nil ||
		inputs.PriceTableRevision != nil ||
		inputs.PriceTableSHA256 != nil
	if run.PriceTableCostCount == 0 {
		if priceFields || run.PriceTable != nil {
			return errors.New(
				"non-calculated cost carries price-table provenance",
			)
		}
		return nil
	}
	if !validAdaptiveBenchmarkPriceTable(run.PriceTable) ||
		inputs.PriceTableID == nil ||
		inputs.PriceTableRevision == nil ||
		inputs.PriceTableSHA256 == nil ||
		*inputs.PriceTableID != run.PriceTable.ID ||
		*inputs.PriceTableRevision != run.PriceTable.Revision ||
		*inputs.PriceTableSHA256 != run.PriceTable.SHA256 {
		return errors.New("calculated cost price-table provenance drifted")
	}
	return nil
}

func validAdaptiveBenchmarkPriceTable(
	table *AdaptiveBenchmarkPriceTable,
) bool {
	return table != nil &&
		validBenchmarkMetadata(table.ID, 128) &&
		table.Revision > 0 &&
		validBenchmarkSHA256(table.SHA256)
}

func cloneAdaptiveBenchmarkPriceTable(
	table *AdaptiveBenchmarkPriceTable,
) *AdaptiveBenchmarkPriceTable {
	if table == nil {
		return nil
	}
	cloned := *table
	return &cloned
}

func benchmarkStringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func validBenchmarkStringUUID(value *string) bool {
	return value != nil && validBenchmarkUUID(*value)
}

func cloneAdaptiveBenchmarkObservation(
	observation *AdaptiveBenchmarkObservation,
) *AdaptiveBenchmarkObservation {
	if observation == nil {
		return nil
	}
	cloned := *observation
	cloned.Value = cloneBenchmarkFloat(observation.Value)
	cloned.ReasonCodes = slices.Clone(observation.ReasonCodes)
	return &cloned
}

func cloneBenchmarkFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
