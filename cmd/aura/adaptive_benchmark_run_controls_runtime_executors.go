package main

import auraeval "github.com/chetto1983/aura/internal/eval"

func (runtime *adaptiveBenchmarkControlRuntime) executors() map[string]adaptiveBenchmarkControlExecutor {
	return map[string]adaptiveBenchmarkControlExecutor{
		"negative_control":         runtime.runNegativeControl,
		"static_equivalence":       runtime.runStaticEquivalence,
		"restart_recovery":         runtime.runRestartRecovery,
		"rollback_static":          runtime.runRollbackStatic,
		"privacy_redaction":        runtime.runPrivacyRedaction,
		"retention_cleanup":        runtime.runRetentionCleanup,
		"same_owner_concurrency":   runtime.runConcurrency,
		"cross_owner_isolation":    runtime.runCrossOwnerIsolation,
		"assignment_write_failure": runtime.runAssignmentWriteFailure,
		"delivery_write_failure":   runtime.runDeliveryWriteFailure,
		"outcome_write_failure":    runtime.runOutcomeWriteFailure,
		"model_transport_failure":  runtime.runModelTransportFailure,
		"memory_epoch_mismatch":    runtime.runMemoryEpochMismatch,
		"settings_apply_failure":   runtime.runSettingsApplyFailure,
		"settings_restore_failure": runtime.runSettingsRestoreFailure,
		"stale_override_recovery":  runtime.runStaleOverrideRecovery,
	}
}

func adaptiveBenchmarkUnexpectedControlCase(
	control auraeval.AdaptiveBenchmarkControlCase,
	expected string,
) error {
	if control.ControlID == expected {
		return nil
	}
	return adaptiveBenchmarkControlError(
		"adaptive benchmark control case drifted",
	)
}

func adaptiveBenchmarkControlError(message string) error {
	return &adaptiveBenchmarkControlRuntimeError{message: message}
}

type adaptiveBenchmarkControlRuntimeError struct {
	message string
}

func (failure *adaptiveBenchmarkControlRuntimeError) Error() string {
	if failure == nil {
		return "adaptive benchmark control failed"
	}
	return failure.message
}
