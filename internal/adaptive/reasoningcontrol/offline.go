package reasoningcontrol

import (
	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent"
)

// NewOffline composes reasoning with immutable run-local inputs.
func NewOffline(
	policies adaptive.PolicyReader,
	snapshots SnapshotLoader,
	recorder EventRecorder,
	providerID string,
	modelID string,
) agent.ReasoningControl {
	source := newRuntimeSourceForEnvironment(
		policies,
		snapshots,
		providerID,
		modelID,
		adaptive.EvaluationOffline,
	)
	if source == nil || recorder == nil {
		return nil
	}
	return newControl(source, recorder)
}
