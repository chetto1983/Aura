package orderingcontrol

import (
	"context"
	"errors"
	"slices"
	"strings"
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

// NewDynamicRecall binds opt-in long-term recall to authoritative policy,
// immutable snapshots, and delayed schema-2 delivery persistence.
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
	decision, err := control.source.decideMemoryRecall(ctx, input)
	if err != nil || decision.Policy.Mode == adaptive.PolicyOff ||
		decision.Policy.Mode == adaptive.PolicyRollback {
		return nil, nil
	}
	assignment, action, err := newMemoryRecallAssignment(input, decision)
	if err != nil {
		return nil, nil
	}
	if _, err := control.recorder.RecordAssignment(ctx, assignment); err != nil {
		return nil, err
	}
	limit, ok := dynamicRecallLimit(action)
	if !ok {
		return nil, errors.New("dynamic recall assigned a non-provider action")
	}
	recall, err := provider(
		ctx,
		input.OwnerID.String(),
		input.Query,
		limit,
	)
	if err != nil {
		delivery, deliveryErr := newMemoryRecallFallbackDelivery(
			assignment,
			adaptive.FallbackStrategyFailed,
			adaptive.StaticActionID,
		)
		if deliveryErr != nil {
			return nil, deliveryErr
		}
		_, recordErr := control.recorder.RecordDelivery(
			ctx,
			assignment.OwnerID,
			assignment.AssignmentID,
			delivery,
		)
		if recordErr != nil {
			return nil, recordErr
		}
		return nil, nil
	}

	var commitMu sync.Mutex
	committed := false
	return &runner.PreparedDynamicRecall{
		Action: action,
		Recall: recall,
		Commit: func(
			ctx context.Context,
			outcome agent.DynamicTailOutcome,
		) error {
			commitMu.Lock()
			defer commitMu.Unlock()
			if committed {
				return nil
			}
			delivery, err := newMemoryRecallDelivery(
				assignment,
				recall,
				outcome,
			)
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
) (adaptive.Assignment, runner.DynamicRecallAction, error) {
	actions := make([]string, len(input.CandidateActions))
	for index, action := range input.CandidateActions {
		actions[index] = string(action)
	}
	if input.OwnerID == uuid.Nil || input.RequestID == uuid.Nil ||
		strings.TrimSpace(input.Query) == "" ||
		input.MaxItems < 4 ||
		!slices.Equal(
			input.CandidateActions,
			runner.DynamicRecallCatalog(true, input.MaxItems),
		) {
		return adaptive.Assignment{}, "", errors.New(
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
		return adaptive.Assignment{}, "", err
	}
	if !slices.Contains(actions, decision.RecommendedAction) {
		return adaptive.Assignment{}, "", errors.New(
			"dynamic recall recommendation is not frozen",
		)
	}
	// Memory recall is opt-in configuration. Treat that explicit operator choice
	// as the deterministic highest configured limit while the global policy
	// still retains off/rollback authority.
	action := input.CandidateActions[len(input.CandidateActions)-1]
	assignmentID, err := adaptive.AssignmentIDForPoint(
		input.OwnerID,
		input.RequestID,
		adaptive.PointMemoryRecall,
		0,
	)
	if err != nil {
		return adaptive.Assignment{}, "", err
	}
	probabilities := deterministicActionProbabilities(actions, string(action))
	eligibilityHash, catalogHash, err := actionCatalogHashes(
		actions,
		probabilities,
	)
	if err != nil {
		return adaptive.Assignment{}, "", err
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
		IntendedActionID:    string(action),
		ActionProbabilities: probabilities,
		SelectionReason:     adaptive.SelectionOperatorOverride,
		Override:            true,
		Features: map[adaptive.FeatureKey]float64{
			adaptive.FeatureQueryLength: float64(
				utf8.RuneCountInString(input.Query),
			),
			adaptive.FeatureRecallLimit: float64(input.MaxItems),
		},
	}
	if _, err := adaptive.NewAssignmentEvent(assignment); err != nil {
		return adaptive.Assignment{}, "", err
	}
	return assignment, action, nil
}

func dynamicRecallLimit(action runner.DynamicRecallAction) (int, bool) {
	switch action {
	case runner.DynamicRecallTop4:
		return 4, true
	case runner.DynamicRecallTop8:
		return 8, true
	default:
		return 0, false
	}
}

func newMemoryRecallDelivery(
	assignment adaptive.Assignment,
	recall runner.DynamicRecall,
	outcome agent.DynamicTailOutcome,
) (adaptive.Delivery, error) {
	if !outcome.Delivered {
		actual := adaptive.StaticActionID
		reason := adaptive.FallbackStateInvalid
		if outcome.FallbackReason == agent.DynamicTailFallbackContextBudget {
			actual = adaptive.ActionNoneID
			reason = adaptive.FallbackContextBudget
		}
		return newMemoryRecallFallbackDelivery(assignment, reason, actual)
	}
	if !recall.Coherent || recall.CorpusEpochBefore == nil ||
		recall.CorpusEpochAfter == nil ||
		*recall.CorpusEpochBefore != *recall.CorpusEpochAfter {
		return adaptive.Delivery{}, errors.New(
			"dynamic recall delivery corpus epoch is incoherent",
		)
	}
	results := make([]adaptive.MemoryRecallResult, len(recall.Results))
	for index, result := range recall.Results {
		results[index] = adaptive.MemoryRecallResult{
			Kind: adaptive.ResultKind(result.Kind),
			ID:   result.ID,
		}
	}
	resultIDs, revisions, err := adaptive.NewMemoryRecallDeliveryMetadata(
		results,
		adaptive.MemoryRecallRevisionValues{
			CorpusEpoch: *recall.CorpusEpochBefore,
			Retriever:   recall.Revisions.Retriever,
			Reranker:    recall.Revisions.Reranker,
			Embedding:   recall.Revisions.Embedding,
			Index:       recall.Revisions.Index,
		},
	)
	if err != nil {
		return adaptive.Delivery{}, err
	}
	probability := float64(1)
	delivery := adaptive.Delivery{
		SchemaVersion:    adaptive.SchemaVersion2,
		AssignmentID:     assignment.AssignmentID,
		IntendedActionID: assignment.IntendedActionID,
		ActualActionID:   assignment.IntendedActionID,
		Status:           adaptive.DeliverySuccess,
		ExposureKnown:    true, ExposureProbability: &probability,
		ResultCount: len(resultIDs), ResultIDs: resultIDs,
		Revisions:       revisions,
		EffectiveLimits: memoryRecallEffectiveLimits(recall.Limits),
	}
	if _, err := adaptive.NewDeliveryEvent(assignment, delivery); err != nil {
		return adaptive.Delivery{}, err
	}
	return delivery, nil
}

func newMemoryRecallFallbackDelivery(
	assignment adaptive.Assignment,
	reason adaptive.FallbackReason,
	actual string,
) (adaptive.Delivery, error) {
	revisions, err := adaptive.NewRevisionSet()
	if err != nil {
		return adaptive.Delivery{}, err
	}
	delivery := adaptive.Delivery{
		SchemaVersion:    adaptive.SchemaVersion2,
		AssignmentID:     assignment.AssignmentID,
		IntendedActionID: assignment.IntendedActionID,
		ActualActionID:   actual,
		Status:           adaptive.DeliveryFallback,
		FallbackReason:   reason,
		ResultIDs:        []adaptive.ResultID{},
		Revisions:        revisions,
		EffectiveLimits:  map[string]int{},
	}
	if _, err := adaptive.NewDeliveryEvent(assignment, delivery); err != nil {
		return adaptive.Delivery{}, err
	}
	return delivery, nil
}

func memoryRecallEffectiveLimits(
	limits map[string]runner.DynamicRecallLimit,
) map[string]int {
	effective := make(map[string]int, len(limits)*3)
	for kind, limit := range limits {
		prefix := strings.TrimPrefix(kind, "memory_")
		effective[prefix+"_requested_k"] = limit.RequestedK
		effective[prefix+"_effective_k"] = limit.EffectiveK
		effective[prefix+"_count"] = limit.Count
	}
	return effective
}

var _ runner.DynamicRecallControl = (*dynamicRecall)(nil)
