package orderingcontrol

import (
	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/runner"
)

// NewOfflineToolDiscovery composes discovery with immutable run-local inputs.
func NewOfflineToolDiscovery(
	policies adaptive.PolicyReader,
	snapshots SnapshotLoader,
	recorder EventRecorder,
	providerID string,
	modelID string,
) tools.ToolDiscoveryControl {
	source := newOfflineRuntimeSource(
		policies,
		snapshots,
		providerID,
		modelID,
	)
	if source == nil || recorder == nil {
		return nil
	}
	return newToolDiscovery(source, recorder)
}

// NewOfflineSkillRouting composes skill routing with immutable run-local inputs.
func NewOfflineSkillRouting(
	policies adaptive.PolicyReader,
	snapshots SnapshotLoader,
	recorder EventRecorder,
	providerID string,
	modelID string,
) tools.SkillRoutingControl {
	source := newOfflineRuntimeSource(
		policies,
		snapshots,
		providerID,
		modelID,
	)
	if source == nil || recorder == nil {
		return nil
	}
	return newSkillRouting(source, recorder)
}

// NewOfflineDocumentRetrieval composes retrieval with immutable run-local inputs.
func NewOfflineDocumentRetrieval(
	policies adaptive.PolicyReader,
	snapshots SnapshotLoader,
	recorder EventRecorder,
	providerID string,
	modelID string,
) tools.DocumentRetrievalControl {
	source := newOfflineRuntimeSource(
		policies,
		snapshots,
		providerID,
		modelID,
	)
	if source == nil || recorder == nil {
		return nil
	}
	return newDocumentRetrieval(source, recorder)
}

// NewOfflineDynamicRecall composes recall with immutable run-local inputs.
func NewOfflineDynamicRecall(
	policies adaptive.PolicyReader,
	snapshots SnapshotLoader,
	recorder EventRecorder,
	providerID string,
	modelID string,
) runner.DynamicRecallControl {
	source := newOfflineRuntimeSource(
		policies,
		snapshots,
		providerID,
		modelID,
	)
	if source == nil || recorder == nil {
		return nil
	}
	return newDynamicRecall(source, recorder)
}
