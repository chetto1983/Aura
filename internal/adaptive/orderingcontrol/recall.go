package orderingcontrol

import (
	"context"
	"errors"
	"slices"
	"sync"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type memoryRecallDecisionSource interface {
	decideMemoryRecall(
		context.Context,
		runner.DynamicRecallInput,
	) (toolDecision, error)
}

type dynamicRecall struct {
	source   memoryRecallDecisionSource
	recorder toolEventRecorder
}

// NewDynamicRecall binds memory-recall diagnostics to authoritative policy,
// immutable snapshots, and schema-2 persistence.
func NewDynamicRecall(
	pool *pgxpool.Pool,
	providerID string,
	modelID string,
) runner.DynamicRecallControl {
	if pool == nil {
		return nil
	}
	return newDynamicRecall(
		newRuntimeSource(
			adaptive.NewPolicyStore(pool),
			adaptive.NewSnapshotStore(pool),
			providerID,
			modelID,
		),
		adaptive.NewStore(pool, adaptive.StoreConfig{}),
	)
}

func newDynamicRecall(
	source memoryRecallDecisionSource,
	recorder toolEventRecorder,
) *dynamicRecall {
	if source == nil || recorder == nil {
		return nil
	}
	return &dynamicRecall{source: source, recorder: recorder}
}

func (control *dynamicRecall) PrepareDynamicRecall(
	ctx context.Context,
	input runner.DynamicRecallInput,
	provider runner.DynamicRecallProvider,
) (*runner.PreparedDynamicRecall, error) {
	if control == nil || control.source == nil ||
		control.recorder == nil || provider == nil {
		return nil, nil
	}
	if input.MaxItems < 4 ||
		slices.Equal(
			input.CandidateActions,
			[]runner.DynamicRecallAction{runner.DynamicRecallStatic},
		) {
		return nil, nil
	}
	decision, err := control.source.decideMemoryRecall(ctx, input)
	if err != nil || decision.Policy.Mode == adaptive.PolicyOff ||
		decision.Policy.Mode == adaptive.PolicyRollback {
		return nil, nil
	}
	assignment, err := newMemoryRecallAssignment(input, decision)
	if err != nil {
		return nil, nil
	}
	if _, err := control.recorder.RecordAssignment(ctx, assignment); err != nil {
		return nil, err
	}

	var commitMu sync.Mutex
	committed := false
	return &runner.PreparedDynamicRecall{
		Action: runner.DynamicRecallStatic,
		Commit: func(
			ctx context.Context,
			_ agent.DynamicTailOutcome,
		) error {
			commitMu.Lock()
			defer commitMu.Unlock()
			if committed {
				return nil
			}
			delivery, err := newMemoryRecallStaticDelivery(assignment)
			if err != nil {
				return err
			}
			if _, err := control.recorder.RecordDelivery(
				ctx,
				assignment.OwnerID,
				assignment.AssignmentID,
				delivery,
			); err != nil {
				return err
			}
			committed = true
			return nil
		},
	}, nil
}

func newMemoryRecallAssignment(
	input runner.DynamicRecallInput,
	decision toolDecision,
) (adaptive.Assignment, error) {
	actions := make([]string, len(input.CandidateActions))
	for index, action := range input.CandidateActions {
		actions[index] = string(action)
	}
	if input.OwnerID == uuid.Nil || input.RequestID == uuid.Nil ||
		input.MaxItems < 4 ||
		!slices.Equal(
			input.CandidateActions,
			runner.DynamicRecallCatalog(true, input.MaxItems),
		) {
		return adaptive.Assignment{}, errors.New(
			"dynamic recall input is not a frozen eligible catalog",
		)
	}
	if err := validateOrderingDecision(
		input.OwnerID,
		adaptive.DomainMemoryRecall,
		adaptive.PointMemoryRecall,
		actions,
		decision,
	); err != nil {
		return adaptive.Assignment{}, err
	}
	if !slices.Contains(actions, decision.RecommendedAction) {
		return adaptive.Assignment{}, errors.New(
			"dynamic recall recommendation is not frozen",
		)
	}
	selectionReason, err := diagnosticSelectionReason(decision.Policy.Mode)
	if err != nil {
		return adaptive.Assignment{}, err
	}
	assignmentID, err := adaptive.AssignmentIDForPoint(
		input.OwnerID,
		input.RequestID,
		adaptive.PointMemoryRecall,
		0,
	)
	if err != nil {
		return adaptive.Assignment{}, err
	}
	probabilities := deterministicActionProbabilities(
		actions,
		adaptive.StaticActionID,
	)
	eligibilityHash, catalogHash, err := actionCatalogHashes(
		actions,
		probabilities,
	)
	if err != nil {
		return adaptive.Assignment{}, err
	}
	assignment := adaptive.Assignment{
		SchemaVersion: adaptive.SchemaVersion2,
		AssignmentID:  assignmentID, OwnerID: input.OwnerID,
		RequestID: input.RequestID, Domain: adaptive.DomainMemoryRecall,
		Point: adaptive.PointMemoryRecall, PointOrdinal: 0,
		PolicyEpoch:   uint64(decision.Policy.Epoch),
		PolicyVersion: decision.Policy.Version, PolicyMode: decision.Policy.Mode,
		SnapshotID:     decision.Snapshot.ID(),
		SnapshotSHA256: decision.Snapshot.SHA256(),
		Environment:    decision.Environment,
		ProviderID:     decision.ProviderID, ModelID: decision.ModelID,
		EligibleActions: actions, EligibilitySHA256: eligibilityHash,
		CatalogSHA256:       catalogHash,
		ChampionActionID:    adaptive.StaticActionID,
		RecommendedActionID: decision.RecommendedAction,
		IntendedActionID:    adaptive.StaticActionID,
		ActionProbabilities: probabilities,
		SelectionReason:     selectionReason,
		Features: map[adaptive.FeatureKey]float64{
			adaptive.FeatureQueryLength: float64(
				utf8.RuneCountInString(input.Query),
			),
			adaptive.FeatureRecallLimit: float64(input.MaxItems),
		},
	}
	if _, err := adaptive.NewAssignmentEvent(assignment); err != nil {
		return adaptive.Assignment{}, err
	}
	return assignment, nil
}

func newMemoryRecallStaticDelivery(
	assignment adaptive.Assignment,
) (adaptive.Delivery, error) {
	revisions, err := adaptive.NewRevisionSet()
	if err != nil {
		return adaptive.Delivery{}, err
	}
	probability := float64(1)
	delivery := adaptive.Delivery{
		SchemaVersion:       adaptive.SchemaVersion2,
		AssignmentID:        assignment.AssignmentID,
		IntendedActionID:    assignment.IntendedActionID,
		ActualActionID:      adaptive.StaticActionID,
		Status:              adaptive.DeliverySuccess,
		ExposureKnown:       true,
		ExposureProbability: &probability,
		ResultIDs:           []adaptive.ResultID{},
		Revisions:           revisions,
		EffectiveLimits:     map[string]int{"top_k": 0},
	}
	if _, err := adaptive.NewDeliveryEvent(assignment, delivery); err != nil {
		return adaptive.Delivery{}, err
	}
	return delivery, nil
}

var _ runner.DynamicRecallControl = (*dynamicRecall)(nil)
