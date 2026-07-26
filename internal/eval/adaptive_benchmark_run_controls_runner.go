package eval

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

// RunAdaptiveBenchmarkControls executes every registered control in order.
func RunAdaptiveBenchmarkControls(
	ctx context.Context,
	driver AdaptiveBenchmarkControlDriver,
) (AdaptiveBenchmarkControlRun, error) {
	if driver == nil {
		return AdaptiveBenchmarkControlRun{}, errors.New(
			"adaptive benchmark control driver is unavailable",
		)
	}
	run := AdaptiveBenchmarkControlRun{
		Records: make(
			[]AdaptiveBenchmarkControlRecord,
			0,
			len(adaptiveBenchmarkControlMatrix),
		),
	}
	var joined error
	for _, controlID := range adaptiveBenchmarkControlMatrix {
		if err := ctx.Err(); err != nil {
			return AdaptiveBenchmarkControlRun{}, err
		}
		controlCase := AdaptiveBenchmarkControlCase{ControlID: controlID}
		if controlID == "same_owner_concurrency" {
			plan := AdaptiveBenchmarkConcurrencySchedule()
			controlCase.ConcurrencyPlan = &plan
		}
		result, driverErr := driver.RunControl(ctx, controlCase)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return AdaptiveBenchmarkControlRun{}, ctxErr
		}
		if driverErr != nil {
			joined = errors.Join(
				joined,
				fmt.Errorf("run control %s: %w", controlID, driverErr),
			)
		}
		if err := validateAdaptiveBenchmarkControlResult(
			controlID,
			result,
		); err != nil {
			joined = errors.Join(joined, err)
		}
		run.Records = append(run.Records, AdaptiveBenchmarkControlRecord{
			Status:      result.Status,
			StartedAt:   result.StartedAt,
			CompletedAt: result.CompletedAt,
			EvidenceIDs: slices.Clone(result.EvidenceIDs),
			ReasonCodes: slices.Clone(result.ReasonCodes),
		})
	}
	if joined != nil {
		return run, joined
	}
	run.verified = true
	return run, nil
}

func validateAdaptiveBenchmarkControlResult(
	controlID string,
	result AdaptiveBenchmarkControlResult,
) error {
	started, startErr := parseAdaptiveBenchmarkTime(result.StartedAt)
	completed, completedErr := parseAdaptiveBenchmarkTime(result.CompletedAt)
	if startErr != nil || completedErr != nil ||
		completed.Before(started) ||
		!validAdaptiveBenchmarkStatus(result.Status) ||
		result.EvidenceIDs == nil ||
		len(result.EvidenceIDs) == 0 ||
		!strictSortedUnique(result.EvidenceIDs) ||
		result.ReasonCodes == nil ||
		!validAdaptiveBenchmarkReasonCodes(result.ReasonCodes) {
		return benchmarkValidationError(
			"control %s result is invalid",
			controlID,
		)
	}
	for _, evidenceID := range result.EvidenceIDs {
		if !validBenchmarkUUID(evidenceID) {
			return benchmarkValidationError(
				"control %s evidence ID is invalid",
				controlID,
			)
		}
	}
	if result.Status == AdaptiveBenchmarkStatusPass &&
		len(result.ReasonCodes) != 0 {
		return benchmarkValidationError(
			"passing control %s has reason codes",
			controlID,
		)
	}
	if result.Status != AdaptiveBenchmarkStatusPass &&
		len(result.ReasonCodes) == 0 {
		return benchmarkValidationError(
			"non-passing control %s omits reason codes",
			controlID,
		)
	}
	wantFaults := adaptiveBenchmarkExpectedFaultReasons(controlID)
	if !slices.Equal(result.ObservedFailureReasonCodes, wantFaults) {
		return benchmarkValidationError(
			"control %s fault evidence is invalid",
			controlID,
		)
	}
	if controlID != "same_owner_concurrency" {
		if result.Concurrency != nil {
			return benchmarkValidationError(
				"control %s has unexpected concurrency evidence",
				controlID,
			)
		}
		return nil
	}
	if result.Concurrency == nil {
		return errors.New(
			"adaptive benchmark concurrency control omits evidence",
		)
	}
	if err := ValidateAdaptiveBenchmarkConcurrencyEvidence(
		*result.Concurrency,
	); err != nil {
		return err
	}
	wantIDs := benchmarkConcurrencyEvidenceIDs(*result.Concurrency)
	if !slices.Equal(result.EvidenceIDs, wantIDs) {
		return errors.New(
			"adaptive benchmark concurrency evidence linkage is incomplete",
		)
	}
	return nil
}

func adaptiveBenchmarkExpectedFaultReasons(controlID string) []string {
	switch controlID {
	case "assignment_write_failure":
		return []string{AdaptiveBenchmarkReasonAssignmentMissing}
	case "delivery_write_failure":
		return []string{AdaptiveBenchmarkReasonDeliveryMissing}
	case "outcome_write_failure":
		return []string{AdaptiveBenchmarkReasonOutcomeMissing}
	case "model_transport_failure":
		return []string{AdaptiveBenchmarkReasonModelTransportFailure}
	case "memory_epoch_mismatch":
		return []string{AdaptiveBenchmarkReasonMemoryEpochMismatch}
	case "settings_apply_failure":
		return []string{AdaptiveBenchmarkReasonSettingsApplyFailure}
	case "settings_restore_failure":
		return []string{AdaptiveBenchmarkReasonSettingsRestoreFailure}
	case "stale_override_recovery":
		return []string{AdaptiveBenchmarkReasonStaleOverrideRecovery}
	default:
		return []string{}
	}
}
