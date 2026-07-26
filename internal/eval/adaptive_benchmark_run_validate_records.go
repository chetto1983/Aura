package eval

import (
	"errors"
	"math"
	"slices"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
)

// Reason codes are bounded so raw provider errors cannot enter the report.
const (
	AdaptiveBenchmarkReasonAssignmentMissing      = "assignment_missing"
	AdaptiveBenchmarkReasonControlFailed          = "control_failed"
	AdaptiveBenchmarkReasonDeliveryMissing        = "delivery_missing"
	AdaptiveBenchmarkReasonDriverFailure          = "driver_failure"
	AdaptiveBenchmarkReasonEvaluatorIneligible    = "evaluator_ineligible"
	AdaptiveBenchmarkReasonEvaluatorMismatch      = "evaluator_mismatch"
	AdaptiveBenchmarkReasonFactLinkageInvalid     = "fact_linkage_invalid"
	AdaptiveBenchmarkReasonMemoryEpochMismatch    = "memory_epoch_mismatch"
	AdaptiveBenchmarkReasonModelParserFailure     = "model_parser_failure"
	AdaptiveBenchmarkReasonModelTransportFailure  = "model_transport_failure"
	AdaptiveBenchmarkReasonOutcomeMissing         = "outcome_missing"
	AdaptiveBenchmarkReasonPrivacyViolation       = "privacy_violation"
	AdaptiveBenchmarkReasonProvenanceMissing      = "provenance_missing"
	AdaptiveBenchmarkReasonRequestMissing         = "request_missing"
	AdaptiveBenchmarkReasonSettingsApplyFailure   = "settings_apply_failure"
	AdaptiveBenchmarkReasonSettingsRestoreFailure = "settings_restore_failure"
	AdaptiveBenchmarkReasonStaleOverrideRecovery  = "stale_override_recovery"
	AdaptiveBenchmarkReasonTimeout                = "timeout"
)

var adaptiveBenchmarkReasonCatalog = []string{
	AdaptiveBenchmarkReasonAnswerMismatch,
	AdaptiveBenchmarkReasonAssignmentMissing,
	AdaptiveBenchmarkReasonControlFailed,
	AdaptiveBenchmarkReasonDeliveryMissing,
	AdaptiveBenchmarkReasonDriverFailure,
	AdaptiveBenchmarkReasonEvaluatorIneligible,
	AdaptiveBenchmarkReasonEvaluatorMismatch,
	AdaptiveBenchmarkReasonFactLinkageInvalid,
	AdaptiveBenchmarkReasonMemoryEpochMismatch,
	AdaptiveBenchmarkReasonModelParserFailure,
	AdaptiveBenchmarkReasonModelTransportFailure,
	AdaptiveBenchmarkReasonOutcomeMissing,
	AdaptiveBenchmarkReasonPrivacyViolation,
	AdaptiveBenchmarkReasonProvenanceMissing,
	AdaptiveBenchmarkReasonRequestMissing,
	AdaptiveBenchmarkReasonResultOrderMismatch,
	AdaptiveBenchmarkReasonSettingsApplyFailure,
	AdaptiveBenchmarkReasonSettingsRestoreFailure,
	AdaptiveBenchmarkReasonSkillMismatch,
	AdaptiveBenchmarkReasonStaleOverrideRecovery,
	AdaptiveBenchmarkReasonTimeout,
	AdaptiveBenchmarkReasonToolMismatch,
}

func validateAdaptiveBenchmarkScenarios(
	scenarios []AdaptiveBenchmarkScenarioRecord,
) error {
	if scenarios == nil || len(scenarios) != 52 {
		return errors.New("adaptive benchmark scenario matrix is incomplete")
	}
	expected := expectedAdaptiveBenchmarkScenarioOrder()
	factIDs := make(map[string]struct{}, len(scenarios)*5)
	for index, scenario := range scenarios {
		if scenario.ScenarioID != expected[index] ||
			scenario.Domain != adaptiveBenchmarkDomainForID(scenario.ScenarioID) {
			return benchmarkValidationError(
				"scenario %d identity or schedule is invalid",
				index,
			)
		}
		if err := validateAdaptiveBenchmarkScenario(scenario); err != nil {
			return benchmarkValidationError(
				"scenario %q: %v",
				scenario.ScenarioID,
				err,
			)
		}
		for _, id := range []*string{
			scenario.RequestID,
			scenario.AssignmentID,
			scenario.DeliveryEventID,
			scenario.QualityOutcomeID,
			scenario.HarmOutcomeID,
		} {
			if id == nil {
				continue
			}
			if _, duplicate := factIDs[*id]; duplicate {
				return benchmarkValidationError(
					"scenario fact ID %q is duplicated",
					*id,
				)
			}
			factIDs[*id] = struct{}{}
		}
	}
	return nil
}

func expectedAdaptiveBenchmarkScenarioOrder() []string {
	scenarios := make(
		[]AdaptiveBenchmarkScenario,
		0,
		len(adaptiveBenchmarkScenarioIDs()),
	)
	for _, scenarioID := range adaptiveBenchmarkScenarioIDs() {
		scenarios = append(scenarios, AdaptiveBenchmarkScenario{
			ScenarioID: scenarioID,
		})
	}
	ordered := AdaptiveBenchmarkScenarioOrder(scenarios)
	ids := make([]string, len(ordered))
	for index, scenario := range ordered {
		ids[index] = scenario.ScenarioID
	}
	return ids
}

func validateAdaptiveBenchmarkScenario(
	scenario AdaptiveBenchmarkScenarioRecord,
) error {
	if !validAdaptiveBenchmarkStatus(scenario.Status) ||
		scenario.ActionProbabilities == nil ||
		scenario.ReasonCodes == nil ||
		!validAdaptiveBenchmarkReasonCodes(scenario.ReasonCodes) {
		return errors.New("status or reason codes are invalid")
	}
	if scenario.Status == AdaptiveBenchmarkStatusPass &&
		len(scenario.ReasonCodes) != 0 {
		return errors.New("passing scenario has reason codes")
	}
	if scenario.Status != AdaptiveBenchmarkStatusPass &&
		len(scenario.ReasonCodes) == 0 {
		return errors.New("non-passing scenario omits reason codes")
	}
	if err := validateAdaptiveBenchmarkLinkage(scenario); err != nil {
		return err
	}
	if err := validateAdaptiveBenchmarkActions(scenario); err != nil {
		return err
	}
	if scenario.PromptTokens < 0 ||
		scenario.CompletionTokens < 0 ||
		scenario.PromptTokens > math.MaxInt64-scenario.CompletionTokens ||
		scenario.TotalTokens !=
			scenario.PromptTokens+scenario.CompletionTokens ||
		scenario.LatencyNS < 0 ||
		(scenario.Status != AdaptiveBenchmarkStatusInvalid &&
			scenario.LatencyNS == 0) {
		return errors.New("tokens or monotonic latency are invalid")
	}
	if scenario.CostUSD != nil &&
		(math.IsNaN(*scenario.CostUSD) ||
			math.IsInf(*scenario.CostUSD, 0) ||
			*scenario.CostUSD < 0 ||
			math.Signbit(*scenario.CostUSD)) {
		return errors.New("cost is invalid")
	}
	if err := validateAdaptiveBenchmarkObservation(
		scenario.Quality,
		true,
	); err != nil {
		return err
	}
	if err := validateAdaptiveBenchmarkQualityDisposition(scenario); err != nil {
		return err
	}
	return validateAdaptiveBenchmarkObservation(scenario.Harm, false)
}

func validateAdaptiveBenchmarkLinkage(
	scenario AdaptiveBenchmarkScenarioRecord,
) error {
	for _, id := range []*string{
		scenario.RequestID,
		scenario.AssignmentID,
		scenario.DeliveryEventID,
		scenario.QualityOutcomeID,
		scenario.HarmOutcomeID,
	} {
		if id != nil && !validBenchmarkUUID(*id) {
			return errors.New("fact linkage contains a noncanonical UUID")
		}
	}
	if (scenario.Quality == nil) != (scenario.QualityOutcomeID == nil) ||
		(scenario.Harm == nil) != (scenario.HarmOutcomeID == nil) {
		return errors.New("outcome linkage does not match observations")
	}
	missingCompletedLinkage := scenario.RequestID == nil ||
		scenario.AssignmentID == nil ||
		scenario.DeliveryEventID == nil ||
		scenario.QualityOutcomeID == nil
	if scenario.Status == AdaptiveBenchmarkStatusPass &&
		missingCompletedLinkage {
		return errors.New("completed scenario fact linkage is incomplete")
	}
	if scenario.Status == AdaptiveBenchmarkStatusFail &&
		adaptiveBenchmarkTerminalFailureReasons(scenario.ReasonCodes) {
		if scenario.RequestID == nil ||
			scenario.AssignmentID == nil ||
			scenario.DeliveryEventID == nil {
			return errors.New(
				"terminal failure omits request, assignment, or delivery",
			)
		}
		return nil
	}
	if scenario.Status == AdaptiveBenchmarkStatusFail &&
		missingCompletedLinkage {
		return errors.New("completed scenario fact linkage is incomplete")
	}
	if scenario.Status == AdaptiveBenchmarkStatusInvalid &&
		missingCompletedLinkage &&
		adaptiveBenchmarkQualityOnlyReasons(scenario.ReasonCodes) {
		return errors.New("invalid missing linkage is not classified")
	}
	return nil
}

func validateAdaptiveBenchmarkActions(
	scenario AdaptiveBenchmarkScenarioRecord,
) error {
	if scenario.Status == AdaptiveBenchmarkStatusInvalid &&
		scenario.ChampionActionID == "" &&
		scenario.RecommendedActionID == "" &&
		scenario.IntendedActionID == "" &&
		scenario.ActualActionID == "" &&
		len(scenario.ActionProbabilities) == 0 {
		return nil
	}
	catalog := adaptiveBenchmarkCatalogForDomain(scenario.Domain)
	if len(catalog) == 0 ||
		len(scenario.ActionProbabilities) != len(catalog) ||
		scenario.ChampionActionID != adaptive.StaticActionID ||
		scenario.IntendedActionID != adaptive.StaticActionID ||
		scenario.ActualActionID != adaptive.StaticActionID ||
		!slices.Contains(catalog, scenario.RecommendedActionID) {
		return errors.New("action identity or static champion is invalid")
	}
	total := float64(0)
	for index, probability := range scenario.ActionProbabilities {
		if probability.ActionID != catalog[index] ||
			math.IsNaN(probability.Probability) ||
			math.IsInf(probability.Probability, 0) ||
			probability.Probability < 0 ||
			probability.Probability > 1 {
			return errors.New("action probability catalog is invalid")
		}
		want := float64(0)
		if probability.ActionID == adaptive.StaticActionID {
			want = 1
		}
		if probability.Probability != want {
			return errors.New("shadow action probability is not static")
		}
		total += probability.Probability
	}
	if total != 1 {
		return errors.New("action probabilities do not sum exactly to one")
	}
	return nil
}

func adaptiveBenchmarkCatalogForDomain(domain adaptive.Domain) []string {
	for _, catalog := range adaptiveBenchmarkActionCatalogs {
		if catalog.Domain == domain {
			return catalog.ActionIDs
		}
	}
	return nil
}

func validateAdaptiveBenchmarkObservation(
	observation *AdaptiveBenchmarkObservation,
	quality bool,
) error {
	if observation == nil {
		return nil
	}
	if !validBenchmarkMetadata(observation.EvaluatorID, 128) ||
		observation.EvaluatorRevision <= 0 ||
		observation.ReasonCodes == nil ||
		!validAdaptiveBenchmarkReasonCodes(observation.ReasonCodes) {
		return errors.New("benchmark observation provenance is invalid")
	}
	if quality &&
		(observation.EvaluatorID != AdaptiveBenchmarkEvaluatorID ||
			observation.EvaluatorRevision !=
				AdaptiveBenchmarkEvaluatorRevision) {
		return errors.New("benchmark quality evaluator is not frozen")
	}
	if observation.Value != nil &&
		(math.IsNaN(*observation.Value) ||
			math.IsInf(*observation.Value, 0)) {
		return errors.New("benchmark observation value is nonfinite")
	}
	if observation.Eligible && observation.Value == nil {
		return errors.New("eligible benchmark observation has no value")
	}
	return nil
}

func validateAdaptiveBenchmarkQualityDisposition(
	scenario AdaptiveBenchmarkScenarioRecord,
) error {
	if scenario.Quality == nil {
		if scenario.Status == AdaptiveBenchmarkStatusPass ||
			(scenario.Status == AdaptiveBenchmarkStatusFail &&
				adaptiveBenchmarkQualityOnlyReasons(
					scenario.ReasonCodes,
				)) {
			return errors.New("quality outcome is missing")
		}
		return nil
	}
	if !slices.Equal(
		scenario.Quality.ReasonCodes,
		scenario.ReasonCodes,
	) {
		return errors.New("quality reasons do not match scenario disposition")
	}
	switch scenario.Status {
	case AdaptiveBenchmarkStatusPass:
		if !scenario.Quality.Eligible ||
			scenario.Quality.Value == nil ||
			*scenario.Quality.Value != 1 {
			return errors.New("passing scenario quality is not one")
		}
	case AdaptiveBenchmarkStatusFail:
		if adaptiveBenchmarkQualityOnlyReasons(scenario.ReasonCodes) &&
			(!scenario.Quality.Eligible ||
				scenario.Quality.Value == nil ||
				*scenario.Quality.Value != 0) {
			return errors.New("quality failure is not a deterministic zero")
		}
	}
	return nil
}

func adaptiveBenchmarkTerminalFailureReasons(reasonCodes []string) bool {
	if len(reasonCodes) == 0 {
		return false
	}
	for _, reasonCode := range reasonCodes {
		if reasonCode != AdaptiveBenchmarkReasonModelParserFailure &&
			reasonCode != AdaptiveBenchmarkReasonModelTransportFailure {
			return false
		}
	}
	return true
}

func validateAdaptiveBenchmarkAggregateBounds(
	scenarios []AdaptiveBenchmarkScenarioRecord,
) error {
	var promptTokens int64
	var completionTokens int64
	var totalTokens int64
	cost := float64(0)
	for _, scenario := range scenarios {
		if promptTokens > math.MaxInt64-scenario.PromptTokens ||
			completionTokens > math.MaxInt64-scenario.CompletionTokens ||
			totalTokens > math.MaxInt64-scenario.TotalTokens {
			return errors.New("adaptive benchmark token summary overflows")
		}
		promptTokens += scenario.PromptTokens
		completionTokens += scenario.CompletionTokens
		totalTokens += scenario.TotalTokens
		if scenario.CostUSD != nil {
			if cost > math.MaxFloat64-*scenario.CostUSD {
				return errors.New("adaptive benchmark cost summary overflows")
			}
			cost += *scenario.CostUSD
		}
	}
	return nil
}

func validateAdaptiveBenchmarkControls(
	controls []AdaptiveBenchmarkControlRecord,
	runStarted time.Time,
	runCompleted time.Time,
) error {
	if controls == nil || len(controls) != len(adaptiveBenchmarkControlMatrix) {
		return errors.New("adaptive benchmark control matrix is incomplete")
	}
	evidence := make(map[string]struct{}, len(controls))
	for index, control := range controls {
		started, startErr := parseAdaptiveBenchmarkTime(control.StartedAt)
		completed, completeErr := parseAdaptiveBenchmarkTime(control.CompletedAt)
		if startErr != nil || completeErr != nil ||
			started.Before(runStarted) ||
			completed.Before(started) ||
			completed.After(runCompleted) ||
			!validAdaptiveBenchmarkStatus(control.Status) ||
			control.EvidenceIDs == nil ||
			control.ReasonCodes == nil ||
			!validAdaptiveBenchmarkReasonCodes(control.ReasonCodes) ||
			!strictSortedUnique(control.EvidenceIDs) {
			return benchmarkValidationError("control %d is invalid", index)
		}
		if control.Status == AdaptiveBenchmarkStatusPass &&
			len(control.ReasonCodes) != 0 {
			return benchmarkValidationError(
				"passing control %d has reason codes",
				index,
			)
		}
		if control.Status != AdaptiveBenchmarkStatusPass &&
			len(control.ReasonCodes) == 0 {
			return benchmarkValidationError(
				"non-passing control %d omits reason codes",
				index,
			)
		}
		if control.Status != AdaptiveBenchmarkStatusInvalid &&
			len(control.EvidenceIDs) == 0 {
			return benchmarkValidationError(
				"control %d omits evidence linkage",
				index,
			)
		}
		for _, id := range control.EvidenceIDs {
			if !validBenchmarkUUID(id) {
				return benchmarkValidationError(
					"control %d evidence ID is invalid",
					index,
				)
			}
			if _, duplicate := evidence[id]; duplicate {
				return errors.New(
					"adaptive benchmark control evidence ID is duplicated",
				)
			}
			evidence[id] = struct{}{}
		}
	}
	return nil
}

func validAdaptiveBenchmarkStatus(status AdaptiveBenchmarkStatus) bool {
	return status == AdaptiveBenchmarkStatusPass ||
		status == AdaptiveBenchmarkStatusFail ||
		status == AdaptiveBenchmarkStatusInvalid
}

func validAdaptiveBenchmarkReasonCodes(reasonCodes []string) bool {
	if !strictSortedUnique(reasonCodes) {
		return false
	}
	for _, reasonCode := range reasonCodes {
		if !slices.Contains(adaptiveBenchmarkReasonCatalog, reasonCode) {
			return false
		}
	}
	return true
}

func strictSortedUnique(values []string) bool {
	for index, value := range values {
		if value == "" || (index > 0 && value <= values[index-1]) {
			return false
		}
	}
	return true
}
