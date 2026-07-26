// Package orderingcontrol binds read-only discovery ports to schema-2 facts.
package orderingcontrol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/google/uuid"
)

var toolActionCatalog = []string{
	tools.ToolDiscoveryBM25,
	tools.ToolDiscoverySemantic,
	tools.ToolDiscoveryStatic,
}

type toolDecision struct {
	Policy            adaptive.Policy
	Snapshot          *adaptive.PolicySnapshot
	Environment       adaptive.EvaluationEnvironment
	RecommendedAction string
	ProviderID        string
	ModelID           string
}

type toolDecisionSource interface {
	decideToolDiscovery(
		context.Context,
		tools.ToolDiscoveryInput,
	) (toolDecision, error)
}

// EventRecorder persists typed assignment and delivery facts.
type EventRecorder interface {
	RecordAssignment(
		context.Context,
		adaptive.Assignment,
	) (int64, error)
	RecordDelivery(
		context.Context,
		uuid.UUID,
		uuid.UUID,
		adaptive.Delivery,
	) (int64, error)
}

type toolEventRecorder = EventRecorder

type toolDiscovery struct {
	source   toolDecisionSource
	recorder toolEventRecorder
}

func newToolDiscovery(
	source toolDecisionSource,
	recorder toolEventRecorder,
) *toolDiscovery {
	if source == nil || recorder == nil {
		return nil
	}
	return &toolDiscovery{source: source, recorder: recorder}
}

func (control *toolDiscovery) OrderToolDiscovery(
	ctx context.Context,
	input tools.ToolDiscoveryInput,
	execute tools.ToolDiscoveryExecutor,
) ([]string, error) {
	if control == nil || control.source == nil {
		return orderFromControl(
			ctx, input, execute, nil, nil,
			newToolAssignment, newToolDelivery,
		)
	}
	return orderFromControl(
		ctx, input, execute,
		control.source.decideToolDiscovery,
		control.recorder,
		newToolAssignment, newToolDelivery,
	)
}

func newToolAssignment(
	input tools.ToolDiscoveryInput,
	decision toolDecision,
) (adaptive.Assignment, error) {
	if input.QueryLength <= 0 || input.MaxResults <= 0 ||
		!slices.IsSorted(input.CandidateIDs) {
		return adaptive.Assignment{}, errors.New(
			"tool discovery input is invalid",
		)
	}
	if _, _, err := adaptive.NewRegisteredCatalogDeliveryMetadata(
		adaptive.ResultTool,
		input.CandidateIDs,
		nil,
		input.CatalogRevision,
	); err != nil {
		return adaptive.Assignment{}, err
	}
	ownerID, ownerErr := uuid.Parse(input.OwnerID)
	requestID, requestErr := uuid.Parse(input.RequestID)
	if ownerErr != nil || requestErr != nil ||
		ownerID == uuid.Nil || requestID == uuid.Nil {
		return adaptive.Assignment{}, errors.New(
			"tool discovery identity is invalid",
		)
	}
	if err := validateToolDecision(ownerID, decision); err != nil {
		return adaptive.Assignment{}, err
	}
	assignmentID, err := adaptive.AssignmentIDForPoint(
		ownerID,
		requestID,
		adaptive.PointToolDiscovery,
		input.PointOrdinal,
	)
	if err != nil {
		return adaptive.Assignment{}, err
	}
	recommended := decision.RecommendedAction
	if !slices.Contains(toolActionCatalog, recommended) {
		return adaptive.Assignment{}, fmt.Errorf(
			"tool discovery recommendation %q is not registered",
			recommended,
		)
	}
	selectionReason, err := diagnosticSelectionReason(decision.Policy.Mode)
	if err != nil {
		return adaptive.Assignment{}, err
	}
	probabilities := deterministicActionProbabilities(
		toolActionCatalog,
		tools.ToolDiscoveryStatic,
	)
	eligibilityHash, catalogHash, err := actionCatalogHashes(
		toolActionCatalog,
		probabilities,
	)
	if err != nil {
		return adaptive.Assignment{}, err
	}
	assignment := adaptive.Assignment{
		SchemaVersion: adaptive.SchemaVersion2,
		AssignmentID:  assignmentID, OwnerID: ownerID, RequestID: requestID,
		Domain: adaptive.DomainToolDiscovery, Point: adaptive.PointToolDiscovery,
		PointOrdinal:   input.PointOrdinal,
		PolicyEpoch:    uint64(decision.Policy.Epoch),
		PolicyVersion:  decision.Policy.Version,
		PolicyMode:     decision.Policy.Mode,
		SnapshotID:     decision.Snapshot.ID(),
		SnapshotSHA256: decision.Snapshot.SHA256(),
		Environment:    decision.Environment,
		ProviderID:     decision.ProviderID, ModelID: decision.ModelID,
		EligibleActions:   slices.Clone(toolActionCatalog),
		EligibilitySHA256: eligibilityHash, CatalogSHA256: catalogHash,
		ChampionActionID:    adaptive.StaticActionID,
		RecommendedActionID: recommended,
		IntendedActionID:    tools.ToolDiscoveryStatic,
		ActionProbabilities: probabilities,
		SelectionReason:     selectionReason,
		Features: map[adaptive.FeatureKey]float64{
			adaptive.FeatureCandidateCount: float64(len(input.CandidateIDs)),
			adaptive.FeatureQueryLength:    float64(input.QueryLength),
			adaptive.FeatureRetrievalLimit: float64(input.MaxResults),
		},
	}
	if _, err := adaptive.NewAssignmentEvent(assignment); err != nil {
		return adaptive.Assignment{}, err
	}
	return assignment, nil
}

func validateToolDecision(
	ownerID uuid.UUID,
	decision toolDecision,
) error {
	return validateOrderingDecision(
		ownerID,
		adaptive.DomainToolDiscovery,
		adaptive.PointToolDiscovery,
		toolActionCatalog,
		decision,
	)
}

func validateOrderingDecision(
	ownerID uuid.UUID,
	domain adaptive.Domain,
	point adaptive.DecisionPoint,
	actionCatalog []string,
	decision toolDecision,
) error {
	if decision.Policy.Epoch <= 0 ||
		decision.Policy.Version == "" ||
		decision.Snapshot == nil {
		return errors.New("adaptive ordering decision state is incomplete")
	}
	scope := decision.Snapshot.Scope()
	if scope.OwnerID != ownerID ||
		scope.Domain != domain ||
		scope.Point != point ||
		scope.ProviderID != decision.ProviderID ||
		scope.ModelID != decision.ModelID ||
		scope.PolicyVersion != decision.Policy.Version {
		return errors.New(
			"adaptive ordering snapshot scope does not match",
		)
	}
	if decision.Snapshot.ChampionActionID() !=
		adaptive.StaticActionID {
		return errors.New(
			"adaptive ordering snapshot champion is not static",
		)
	}
	actions := decision.Snapshot.Actions()
	actionIDs := make([]string, len(actions))
	for index, action := range actions {
		actionIDs[index] = action.ActionID
	}
	if !slices.Equal(actionIDs, actionCatalog) {
		return errors.New(
			"adaptive ordering snapshot action catalog is not frozen",
		)
	}
	return nil
}

func diagnosticSelectionReason(
	mode adaptive.PolicyMode,
) (adaptive.SelectionReason, error) {
	switch mode {
	case adaptive.PolicyShadow:
		return adaptive.SelectionShadowStatic, nil
	case adaptive.PolicyCanary:
		return adaptive.SelectionCanaryDiagnostic, nil
	case adaptive.PolicyActive:
		return adaptive.SelectionUnsupported, nil
	default:
		return "", fmt.Errorf(
			"adaptive policy mode %q cannot emit a discovery assignment",
			mode,
		)
	}
}

func deterministicActionProbabilities(
	actions []string,
	selected string,
) []adaptive.ActionProbability {
	probabilities := make([]adaptive.ActionProbability, len(actions))
	for index, action := range actions {
		probability := float64(0)
		if action == selected {
			probability = 1
		}
		probabilities[index] = adaptive.ActionProbability{
			ActionID: action, Probability: probability,
		}
	}
	return probabilities
}

func actionCatalogHashes(
	actions []string,
	probabilities []adaptive.ActionProbability,
) (string, string, error) {
	eligibility, err := json.Marshal(actions)
	if err != nil {
		return "", "", err
	}
	catalog, err := json.Marshal(probabilities)
	if err != nil {
		return "", "", err
	}
	eligibilitySum := sha256.Sum256(eligibility)
	catalogSum := sha256.Sum256(catalog)
	return hex.EncodeToString(eligibilitySum[:]),
		hex.EncodeToString(catalogSum[:]),
		nil
}

func newToolDelivery(
	input tools.ToolDiscoveryInput,
	assignment adaptive.Assignment,
	ids []string,
) (adaptive.Delivery, error) {
	results, revisions, err :=
		adaptive.NewRegisteredCatalogDeliveryMetadata(
			adaptive.ResultTool,
			input.CandidateIDs,
			ids,
			input.CatalogRevision,
		)
	if err != nil {
		return adaptive.Delivery{}, err
	}
	probability := float64(1)
	return adaptive.Delivery{
		SchemaVersion:    adaptive.SchemaVersion2,
		AssignmentID:     assignment.AssignmentID,
		IntendedActionID: assignment.IntendedActionID,
		ActualActionID:   assignment.IntendedActionID,
		Status:           adaptive.DeliverySuccess,
		ExposureKnown:    true, ExposureProbability: &probability,
		ResultCount: len(ids), ResultIDs: results, Revisions: revisions,
		EffectiveLimits: map[string]int{"max_results": input.MaxResults},
	}, nil
}

var _ tools.ToolDiscoveryControl = (*toolDiscovery)(nil)
