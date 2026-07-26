// Package reasoningcontrol binds the agent's per-round port to schema-2 facts.
package reasoningcontrol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

var actionCatalog = []string{
	string(agent.ReasoningActionHigh),
	string(agent.ReasoningActionLow),
	string(agent.ReasoningActionNone),
	string(agent.ReasoningActionStatic),
}

// Decision supplies verified policy state and a diagnostic recommendation.
type Decision struct {
	Policy          adaptive.Policy
	Snapshot        *adaptive.PolicySnapshot
	Environment     adaptive.EvaluationEnvironment
	RecommendedTier prompt.ReasoningTier
}

// DecisionSource reads policy and immutable local snapshot state.
type DecisionSource interface {
	DecideReasoning(context.Context, agent.ReasoningControlInput) (Decision, error)
}

// EventRecorder is satisfied by adaptive.Store's typed persistence API.
type EventRecorder interface {
	RecordAssignment(context.Context, adaptive.Assignment) (int64, error)
	RecordDelivery(
		context.Context,
		uuid.UUID,
		uuid.UUID,
		adaptive.Delivery,
	) (int64, error)
}

// Control coordinates one reasoning assignment and delivery.
type Control struct {
	source   DecisionSource
	recorder EventRecorder
}

func newControl(source DecisionSource, recorder EventRecorder) *Control {
	if source == nil || recorder == nil {
		return nil
	}
	return &Control{source: source, recorder: recorder}
}

// DecideAndDeliverReasoning implements agent.ReasoningControl.
func (control *Control) DecideAndDeliverReasoning(
	ctx context.Context,
	input agent.ReasoningControlInput,
	learned agent.ReasoningRequestBuilder,
	static agent.StaticReasoningRequestBuilder,
) (agent.PreparedReasoningRequest, error) {
	if control == nil || control.source == nil || control.recorder == nil {
		return static(ctx)
	}
	decision, err := control.source.DecideReasoning(ctx, input)
	if err != nil || decision.Policy.Mode == adaptive.PolicyOff ||
		decision.Policy.Mode == adaptive.PolicyRollback {
		return static(ctx)
	}
	assignment, err := newAssignment(input, decision)
	if err != nil {
		return static(ctx)
	}

	var intercepted *agent.PreparedReasoningRequest
	var delivery adaptive.Delivery
	return adaptive.DecideAndDeliver(
		ctx,
		assignment,
		func(ctx context.Context, _ adaptive.Event) error {
			_, err := control.recorder.RecordAssignment(ctx, assignment)
			return err
		},
		func(
			ctx context.Context,
			assignment adaptive.Assignment,
		) (adaptive.Exposure[agent.PreparedReasoningRequest], error) {
			prepared, err := learned(
				ctx, agent.ReasoningAction(assignment.IntendedActionID),
			)
			if err != nil {
				return adaptive.Exposure[agent.PreparedReasoningRequest]{}, err
			}
			if prepared.HookResult != nil {
				intercepted = &prepared
				return adaptive.Exposure[agent.PreparedReasoningRequest]{},
					errors.New("reasoning request intercepted before model delivery")
			}
			delivery, err = newDelivery(assignment, prepared.Request.MaxTokens)
			if err != nil {
				return adaptive.Exposure[agent.PreparedReasoningRequest]{}, err
			}
			return adaptive.Exposure[agent.PreparedReasoningRequest]{
				Value: prepared, Delivery: delivery,
			}, nil
		},
		func(ctx context.Context, _ adaptive.Event) error {
			_, err := control.recorder.RecordDelivery(
				ctx, assignment.OwnerID, assignment.AssignmentID, delivery,
			)
			return err
		},
		func(context.Context) (agent.PreparedReasoningRequest, error) {
			if intercepted != nil {
				return *intercepted, nil
			}
			return static(ctx)
		},
	)
}

func newAssignment(
	input agent.ReasoningControlInput,
	decision Decision,
) (adaptive.Assignment, error) {
	if err := validateDecision(input, decision); err != nil {
		return adaptive.Assignment{}, err
	}
	assignmentID, err := adaptive.AssignmentIDForPoint(
		input.OwnerID, input.RequestID, adaptive.PointReasoning, input.PointOrdinal,
	)
	if err != nil {
		return adaptive.Assignment{}, err
	}
	recommendedAction := string(agent.ReasoningActionStatic)
	if decision.RecommendedTier.Valid() {
		recommendedAction, _ = actionForTier(decision.RecommendedTier)
	}
	intendedAction := string(agent.ReasoningActionStatic)
	var selectionReason adaptive.SelectionReason
	override := false
	if input.Override != "" {
		tier := prompt.ReasoningTier(input.Override)
		var ok bool
		intendedAction, ok = actionForTier(tier)
		if !ok {
			return adaptive.Assignment{}, fmt.Errorf(
				"reasoning override %q is outside the frozen action catalog",
				input.Override,
			)
		}
		recommendedAction = intendedAction
		selectionReason = adaptive.SelectionOperatorOverride
		override = true
	} else {
		switch decision.Policy.Mode {
		case adaptive.PolicyShadow:
			selectionReason = adaptive.SelectionShadowStatic
		case adaptive.PolicyCanary:
			selectionReason = adaptive.SelectionCanaryDiagnostic
		case adaptive.PolicyActive:
			selectionReason = adaptive.SelectionUnsupported
		default:
			return adaptive.Assignment{}, fmt.Errorf(
				"reasoning policy mode %q cannot emit an assignment",
				decision.Policy.Mode,
			)
		}
	}
	probabilities := deterministicProbabilities(intendedAction)
	eligibilitySHA256, catalogSHA256, err := catalogHashes(
		actionCatalog, probabilities,
	)
	if err != nil {
		return adaptive.Assignment{}, err
	}
	assignment := adaptive.Assignment{
		SchemaVersion: adaptive.SchemaVersion2, AssignmentID: assignmentID,
		OwnerID: input.OwnerID, RequestID: input.RequestID,
		Domain: adaptive.DomainReasoning, Point: adaptive.PointReasoning,
		PointOrdinal:  input.PointOrdinal,
		PolicyEpoch:   uint64(decision.Policy.Epoch),
		PolicyVersion: decision.Policy.Version, PolicyMode: decision.Policy.Mode,
		SnapshotID:     decision.Snapshot.ID(),
		SnapshotSHA256: decision.Snapshot.SHA256(),
		Environment:    decision.Environment,
		ProviderID:     input.ProviderID, ModelID: input.ModelID,
		EligibleActions:   slices.Clone(actionCatalog),
		EligibilitySHA256: eligibilitySHA256, CatalogSHA256: catalogSHA256,
		ChampionActionID:    string(agent.ReasoningActionStatic),
		RecommendedActionID: recommendedAction, IntendedActionID: intendedAction,
		ActionProbabilities: probabilities, SelectionReason: selectionReason,
		Override: override,
		Features: map[adaptive.FeatureKey]float64{
			adaptive.FeatureMessageCount: float64(input.MessageCount),
		},
	}
	if _, err := adaptive.NewAssignmentEvent(assignment); err != nil {
		return adaptive.Assignment{}, err
	}
	return assignment, nil
}

func validateDecision(
	input agent.ReasoningControlInput,
	decision Decision,
) error {
	if decision.Policy.Epoch <= 0 || decision.Policy.Version == "" ||
		decision.Snapshot == nil {
		return errors.New("reasoning decision state is incomplete")
	}
	scope := decision.Snapshot.Scope()
	if scope.OwnerID != input.OwnerID ||
		scope.Domain != adaptive.DomainReasoning ||
		scope.Point != adaptive.PointReasoning ||
		scope.ProviderID != input.ProviderID ||
		scope.ModelID != input.ModelID ||
		scope.PolicyVersion != decision.Policy.Version {
		return errors.New("reasoning decision snapshot scope does not match the model round")
	}
	if decision.Snapshot.ChampionActionID() !=
		string(agent.ReasoningActionStatic) {
		return errors.New("reasoning decision snapshot champion is not static")
	}
	actions := decision.Snapshot.Actions()
	actionIDs := make([]string, len(actions))
	for index, action := range actions {
		actionIDs[index] = action.ActionID
	}
	if !slices.Equal(actionIDs, actionCatalog) {
		return errors.New("reasoning decision snapshot action catalog is not frozen")
	}
	if decision.RecommendedTier != "" && !decision.RecommendedTier.Valid() {
		return errors.New("reasoning decision recommendation is unsupported")
	}
	if llm.ReasoningTarget(input.ProviderID, input.BaseURL) ==
		llm.ReasoningTargetNone {
		return errors.New("reasoning decision provider has no supported reasoning parameter")
	}
	return nil
}

func actionForTier(tier prompt.ReasoningTier) (string, bool) {
	switch tier {
	case prompt.ReasoningTierHigh:
		return string(agent.ReasoningActionHigh), true
	case prompt.ReasoningTierLow:
		return string(agent.ReasoningActionLow), true
	case prompt.ReasoningTierNone:
		return string(agent.ReasoningActionNone), true
	default:
		return "", false
	}
}

func deterministicProbabilities(
	intendedAction string,
) []adaptive.ActionProbability {
	probabilities := make([]adaptive.ActionProbability, len(actionCatalog))
	for index, actionID := range actionCatalog {
		probability := float64(0)
		if actionID == intendedAction {
			probability = 1
		}
		probabilities[index] = adaptive.ActionProbability{
			ActionID: actionID, Probability: probability,
		}
	}
	return probabilities
}

func catalogHashes(
	actions []string,
	probabilities []adaptive.ActionProbability,
) (string, string, error) {
	eligibility, err := json.Marshal(actions)
	if err != nil {
		return "", "", fmt.Errorf("marshal reasoning eligibility: %w", err)
	}
	catalog, err := json.Marshal(probabilities)
	if err != nil {
		return "", "", fmt.Errorf("marshal reasoning catalog: %w", err)
	}
	eligibilitySum := sha256.Sum256(eligibility)
	catalogSum := sha256.Sum256(catalog)
	return hex.EncodeToString(eligibilitySum[:]),
		hex.EncodeToString(catalogSum[:]), nil
}

func newDelivery(
	assignment adaptive.Assignment,
	maxTokens int,
) (adaptive.Delivery, error) {
	probability := float64(0)
	for _, candidate := range assignment.ActionProbabilities {
		if candidate.ActionID == assignment.IntendedActionID {
			probability = candidate.Probability
			break
		}
	}
	revisions, err := adaptive.NewRevisionSet()
	if err != nil {
		return adaptive.Delivery{}, err
	}
	return adaptive.Delivery{
		SchemaVersion:    adaptive.SchemaVersion2,
		AssignmentID:     assignment.AssignmentID,
		IntendedActionID: assignment.IntendedActionID,
		ActualActionID:   assignment.IntendedActionID,
		Status:           adaptive.DeliverySuccess,
		ExposureKnown:    true, ExposureProbability: &probability,
		ResultIDs: []adaptive.ResultID{}, Revisions: revisions,
		EffectiveLimits: map[string]int{"max_tokens": maxTokens},
	}, nil
}

var _ agent.ReasoningControl = (*Control)(nil)
