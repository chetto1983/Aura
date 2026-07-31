package orderingcontrol

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type retrievalDecisionSource interface {
	decideDocumentRetrieval(
		context.Context,
		tools.DocumentRetrievalInput,
	) (toolDecision, error)
}

type documentRetrieval struct {
	source   retrievalDecisionSource
	recorder EventRecorder
}

func newDocumentRetrieval(
	source retrievalDecisionSource,
	recorder EventRecorder,
) *documentRetrieval {
	if source == nil || recorder == nil {
		return nil
	}
	return &documentRetrieval{source: source, recorder: recorder}
}

// NewDocumentRetrieval binds versioned plans to authoritative policy,
// snapshots, and schema-2 persistence.
func NewDocumentRetrieval(
	pool *pgxpool.Pool,
	providerID string,
	modelID string,
) tools.DocumentRetrievalControl {
	if pool == nil {
		return nil
	}
	return newDocumentRetrieval(
		newRuntimeSource(
			adaptive.NewPolicyStore(pool),
			adaptive.NewSnapshotStore(pool),
			providerID,
			modelID,
		),
		adaptive.NewStore(pool, adaptive.StoreConfig{}),
	)
}

func (control *documentRetrieval) RetrieveDocuments(
	ctx context.Context,
	input tools.DocumentRetrievalInput,
	execute tools.DocumentRetrievalExecutor,
	static tools.DocumentRetrievalExecutor,
) (documents.RetrievalResult, error) {
	fallback := func(context.Context) (documents.RetrievalResult, error) {
		if static == nil {
			return documents.RetrievalResult{},
				errors.New("adaptive retrieval requires a static plan")
		}
		return static(ctx, documents.RetrievalPlanStatic)
	}
	if control == nil || control.source == nil ||
		control.recorder == nil || execute == nil {
		return fallback(ctx)
	}
	decision, err := control.source.decideDocumentRetrieval(ctx, input)
	if err != nil || decision.Policy.Mode == adaptive.PolicyOff ||
		decision.Policy.Mode == adaptive.PolicyRollback {
		return fallback(ctx)
	}
	assignment, err := newRetrievalAssignment(input, decision)
	if err != nil {
		return fallback(ctx)
	}
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
		) (adaptive.Exposure[documents.RetrievalResult], error) {
			result, err := execute(ctx, assignment.IntendedActionID)
			if err != nil {
				return adaptive.Exposure[documents.RetrievalResult]{}, err
			}
			delivery, err = newRetrievalDelivery(input, assignment, result)
			if err != nil {
				return adaptive.Exposure[documents.RetrievalResult]{}, err
			}
			return adaptive.Exposure[documents.RetrievalResult]{
				Value: result, Delivery: delivery,
			}, nil
		},
		func(ctx context.Context, _ adaptive.Event) error {
			_, err := control.recorder.RecordDelivery(
				ctx, assignment.OwnerID, assignment.AssignmentID, delivery,
			)
			return err
		},
		fallback,
	)
}

func newRetrievalAssignment(
	input tools.DocumentRetrievalInput,
	decision toolDecision,
) (adaptive.Assignment, error) {
	if input.QueryLength <= 0 || input.MaxResults <= 0 ||
		!slices.IsSorted(input.PlanIDs) ||
		!slices.Contains(input.PlanIDs, documents.RetrievalPlanStatic) ||
		!slices.Equal(input.PlanIDs, input.Frozen.PlanIDs()) ||
		input.CatalogRevision != input.Frozen.CatalogRevision() {
		return adaptive.Assignment{}, errors.New(
			"document retrieval input is not a frozen catalog",
		)
	}
	ownerID, ownerErr := uuid.Parse(input.OwnerID)
	requestID, requestErr := uuid.Parse(input.RequestID)
	if ownerErr != nil || requestErr != nil ||
		ownerID == uuid.Nil || requestID == uuid.Nil {
		return adaptive.Assignment{},
			errors.New("document retrieval identity is invalid")
	}
	if err := validateOrderingDecision(
		ownerID,
		adaptive.DomainKnowledge,
		adaptive.PointKnowledge,
		input.PlanIDs,
		decision,
	); err != nil {
		return adaptive.Assignment{}, err
	}
	if !slices.Contains(input.PlanIDs, decision.RecommendedAction) {
		return adaptive.Assignment{}, fmt.Errorf(
			"document retrieval recommendation %q is not frozen",
			decision.RecommendedAction,
		)
	}
	assignmentID, err := adaptive.AssignmentIDForPoint(
		ownerID, requestID, adaptive.PointKnowledge, input.PointOrdinal,
	)
	if err != nil {
		return adaptive.Assignment{}, err
	}
	selectionReason, err := diagnosticSelectionReason(decision.Policy.Mode)
	if err != nil {
		return adaptive.Assignment{}, err
	}
	probabilities := deterministicActionProbabilities(
		input.PlanIDs, documents.RetrievalPlanStatic,
	)
	eligibilityHash, catalogHash, err := actionCatalogHashes(
		input.PlanIDs, probabilities,
	)
	if err != nil {
		return adaptive.Assignment{}, err
	}
	assignment := adaptive.Assignment{
		SchemaVersion: adaptive.SchemaVersion2,
		AssignmentID:  assignmentID, OwnerID: ownerID, RequestID: requestID,
		Domain: adaptive.DomainKnowledge, Point: adaptive.PointKnowledge,
		PointOrdinal:  input.PointOrdinal,
		PolicyEpoch:   uint64(decision.Policy.Epoch),
		PolicyVersion: decision.Policy.Version, PolicyMode: decision.Policy.Mode,
		SnapshotID:     decision.Snapshot.ID(),
		SnapshotSHA256: decision.Snapshot.SHA256(),
		Environment:    decision.Environment,
		ProviderID:     decision.ProviderID, ModelID: decision.ModelID,
		EligibleActions:   slices.Clone(input.PlanIDs),
		EligibilitySHA256: eligibilityHash, CatalogSHA256: catalogHash,
		ChampionActionID:    adaptive.StaticActionID,
		RecommendedActionID: decision.RecommendedAction,
		IntendedActionID:    documents.RetrievalPlanStatic,
		ActionProbabilities: probabilities, SelectionReason: selectionReason,
		Features: map[adaptive.FeatureKey]float64{
			adaptive.FeatureCandidateCount: float64(len(input.PlanIDs)),
			adaptive.FeatureQueryLength:    float64(input.QueryLength),
			adaptive.FeatureRetrievalLimit: float64(input.MaxResults),
		},
	}
	if _, err := adaptive.NewAssignmentEvent(assignment); err != nil {
		return adaptive.Assignment{}, err
	}
	return assignment, nil
}

func newRetrievalDelivery(
	input tools.DocumentRetrievalInput,
	assignment adaptive.Assignment,
	result documents.RetrievalResult,
) (adaptive.Delivery, error) {
	req := documents.SearchRequest{
		DocumentID: input.DocumentID, Limit: input.MaxResults,
		IdentityID: input.OwnerID,
	}
	if err := input.Frozen.ValidateResult(result, req); err != nil {
		return adaptive.Delivery{}, err
	}
	hits := result.Flatten()
	ids := make([]string, len(hits))
	for index := range hits {
		ids[index] = hits[index].ChunkID
	}
	plan, ok := input.Frozen.Plan(result.PlanID)
	if !ok || result.PlanID != assignment.IntendedActionID {
		return adaptive.Delivery{},
			errors.New("document retrieval result does not match assignment")
	}
	results, revisions, err := adaptive.NewRetrievalDeliveryMetadata(
		ids,
		ids,
		adaptive.RetrievalRevisionValues{
			CorpusEpoch: plan.Revisions.CorpusEpoch,
			Parser:      plan.Revisions.Parser, Chunker: plan.Revisions.Chunker,
			Embedding: plan.Revisions.Embedding, Index: plan.Revisions.Index,
			Reranker: plan.Revisions.Reranker, Retriever: plan.Revisions.Retriever,
		},
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
		EffectiveLimits: map[string]int{
			"max_results": input.MaxResults,
		},
	}, nil
}

var _ tools.DocumentRetrievalControl = (*documentRetrieval)(nil)
