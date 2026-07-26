package adaptive

// EvaluationEnvironment identifies where evidence was collected.
type EvaluationEnvironment string

const (
	// EvaluationSpike marks synthetic pre-production evidence.
	EvaluationSpike EvaluationEnvironment = "spike"
	// EvaluationOffline marks replay or benchmark evidence.
	EvaluationOffline EvaluationEnvironment = "offline"
	// EvaluationProductionCanary marks evidence observed from a live canary.
	EvaluationProductionCanary EvaluationEnvironment = "production_canary"
)
