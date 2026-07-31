package orderingcontrol

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var skillActionCatalog = []string{
	tools.SkillRoutingCatalog,
	tools.SkillRoutingStatic,
}

type skillDecision = toolDecision

type skillDecisionSource interface {
	decideSkillRouting(
		context.Context,
		tools.SkillRoutingInput,
	) (skillDecision, error)
}

type skillRouting struct {
	source   skillDecisionSource
	recorder EventRecorder
}

func newSkillRouting(
	source skillDecisionSource,
	recorder EventRecorder,
) *skillRouting {
	if source == nil || recorder == nil {
		return nil
	}
	return &skillRouting{source: source, recorder: recorder}
}

// NewSkillRouting binds typed queried-list decisions to authoritative runtime
// policy, immutable snapshots, and schema-2 ledger persistence.
func NewSkillRouting(
	pool *pgxpool.Pool,
	providerID string,
	modelID string,
) tools.SkillRoutingControl {
	if pool == nil {
		return nil
	}
	source := newRuntimeSource(
		adaptive.NewPolicyStore(pool),
		adaptive.NewSnapshotStore(pool),
		providerID,
		modelID,
	)
	return newSkillRouting(
		source,
		adaptive.NewStore(pool, adaptive.StoreConfig{}),
	)
}

func (control *skillRouting) OrderSkillRouting(
	ctx context.Context,
	input tools.SkillRoutingInput,
	execute tools.SkillRoutingExecutor,
) ([]string, error) {
	if control == nil || control.source == nil {
		return orderFromControl(
			ctx, input, execute, nil, nil,
			newSkillAssignment, newSkillDelivery,
		)
	}
	return orderFromControl(
		ctx, input, execute,
		control.source.decideSkillRouting,
		control.recorder,
		newSkillAssignment, newSkillDelivery,
	)
}

func newSkillAssignment(
	input tools.SkillRoutingInput,
	decision skillDecision,
) (adaptive.Assignment, error) {
	if input.QueryLength <= 0 {
		return adaptive.Assignment{}, errors.New(
			"skill routing input is invalid",
		)
	}
	if _, _, err := adaptive.NewRegisteredCatalogDeliveryMetadata(
		adaptive.ResultSkill,
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
			"skill routing identity is invalid",
		)
	}
	if err := validateOrderingDecision(
		ownerID,
		adaptive.DomainSkillRouting,
		adaptive.PointSkillRouting,
		skillActionCatalog,
		decision,
	); err != nil {
		return adaptive.Assignment{}, err
	}
	if !slices.Contains(
		skillActionCatalog,
		decision.RecommendedAction,
	) {
		return adaptive.Assignment{}, fmt.Errorf(
			"skill routing recommendation %q is not registered",
			decision.RecommendedAction,
		)
	}
	assignmentID, err := adaptive.AssignmentIDForPoint(
		ownerID,
		requestID,
		adaptive.PointSkillRouting,
		input.PointOrdinal,
	)
	if err != nil {
		return adaptive.Assignment{}, err
	}
	selectionReason, err := diagnosticSelectionReason(
		decision.Policy.Mode,
	)
	if err != nil {
		return adaptive.Assignment{}, err
	}
	probabilities := deterministicActionProbabilities(
		skillActionCatalog,
		tools.SkillRoutingStatic,
	)
	eligibilityHash, catalogHash, err := actionCatalogHashes(
		skillActionCatalog,
		probabilities,
	)
	if err != nil {
		return adaptive.Assignment{}, err
	}
	assignment := adaptive.Assignment{
		SchemaVersion:       adaptive.SchemaVersion2,
		AssignmentID:        assignmentID,
		OwnerID:             ownerID,
		RequestID:           requestID,
		Domain:              adaptive.DomainSkillRouting,
		Point:               adaptive.PointSkillRouting,
		PointOrdinal:        input.PointOrdinal,
		PolicyEpoch:         uint64(decision.Policy.Epoch),
		PolicyVersion:       decision.Policy.Version,
		PolicyMode:          decision.Policy.Mode,
		SnapshotID:          decision.Snapshot.ID(),
		SnapshotSHA256:      decision.Snapshot.SHA256(),
		Environment:         decision.Environment,
		ProviderID:          decision.ProviderID,
		ModelID:             decision.ModelID,
		EligibleActions:     slices.Clone(skillActionCatalog),
		EligibilitySHA256:   eligibilityHash,
		CatalogSHA256:       catalogHash,
		ChampionActionID:    adaptive.StaticActionID,
		RecommendedActionID: decision.RecommendedAction,
		IntendedActionID:    tools.SkillRoutingStatic,
		ActionProbabilities: probabilities,
		SelectionReason:     selectionReason,
		Features: map[adaptive.FeatureKey]float64{
			adaptive.FeatureQueryLength: float64(input.QueryLength),
			adaptive.FeatureSkillCandidateCount: float64(
				len(input.CandidateIDs),
			),
		},
	}
	if _, err := adaptive.NewAssignmentEvent(assignment); err != nil {
		return adaptive.Assignment{}, err
	}
	return assignment, nil
}

func newSkillDelivery(
	input tools.SkillRoutingInput,
	assignment adaptive.Assignment,
	ids []string,
) (adaptive.Delivery, error) {
	results, revisions, err :=
		adaptive.NewRegisteredCatalogDeliveryMetadata(
			adaptive.ResultSkill,
			input.CandidateIDs,
			ids,
			input.CatalogRevision,
		)
	if err != nil {
		return adaptive.Delivery{}, err
	}
	probability := float64(1)
	return adaptive.Delivery{
		SchemaVersion:       adaptive.SchemaVersion2,
		AssignmentID:        assignment.AssignmentID,
		IntendedActionID:    assignment.IntendedActionID,
		ActualActionID:      assignment.IntendedActionID,
		Status:              adaptive.DeliverySuccess,
		ExposureKnown:       true,
		ExposureProbability: &probability,
		ResultCount:         len(ids),
		ResultIDs:           results,
		Revisions:           revisions,
		EffectiveLimits: map[string]int{
			"skill_limit": len(input.CandidateIDs),
		},
	}, nil
}

var _ tools.SkillRoutingControl = (*skillRouting)(nil)
