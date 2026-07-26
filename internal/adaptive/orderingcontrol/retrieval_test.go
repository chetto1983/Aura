package orderingcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/rerank"
	"github.com/google/uuid"
)

type retrievalDecisionSourceFunc func(
	context.Context,
	tools.DocumentRetrievalInput,
) (toolDecision, error)

func (fn retrievalDecisionSourceFunc) decideDocumentRetrieval(
	ctx context.Context,
	input tools.DocumentRetrievalInput,
) (toolDecision, error) {
	return fn(ctx, input)
}

func TestDocumentRetrievalPersistsExactTypedExposureAroundPlanIO(t *testing.T) {
	input := retrievalInput(t)
	decision := retrievalShadowDecision(t, input)
	var order []string
	recorder := &recordingToolEvents{order: &order}
	control := newDocumentRetrieval(
		retrievalDecisionSourceFunc(func(
			context.Context,
			tools.DocumentRetrievalInput,
		) (toolDecision, error) {
			order = append(order, "select")
			return decision, nil
		}),
		recorder,
	)
	execute := func(
		context.Context,
		string,
	) (documents.RetrievalResult, error) {
		order = append(order, "io")
		return documents.RetrievalResult{
			PlanID: documents.RetrievalPlanStatic,
			Hits: []documents.SearchHit{
				{DocumentID: "doc-7", ChunkID: "chunk-doc-7-000001"},
				{DocumentID: "doc-7", ChunkID: "chunk-doc-7-000002"},
			},
			EffectiveLimit: 5,
		}, nil
	}
	result, err := control.RetrieveDocuments(
		t.Context(), input, execute, execute,
	)
	order = append(order, "exposure")
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Flatten(); len(got) != 2 {
		t.Fatalf("results = %v", got)
	}
	if want := []string{"select", "assignment", "io", "delivery", "exposure"}; !slices.Equal(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	if len(recorder.assignments) != 1 || len(recorder.deliveries) != 1 {
		t.Fatalf(
			"facts = %d assignments/%d deliveries",
			len(recorder.assignments), len(recorder.deliveries),
		)
	}
	var assignment adaptive.Assignment
	for _, assignment = range recorder.assignments {
	}
	if assignment.Domain != adaptive.DomainKnowledge ||
		assignment.Point != adaptive.PointKnowledge ||
		assignment.IntendedActionID != documents.RetrievalPlanStatic ||
		!slices.Equal(assignment.EligibleActions, input.PlanIDs) {
		t.Fatalf("assignment = %+v", assignment)
	}
	payload, err := json.Marshal(recorder.deliveries[assignment.AssignmentID])
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, want := range []string{
		`"kind":"chunk","id":"chunk-doc-7-000001"`,
		`"kind":"chunk","id":"chunk-doc-7-000002"`,
		`"parser":"documents-parser-v1"`,
		`"chunker":"documents-chunker-v1"`,
		`"corpus":"9"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("delivery %s does not contain %q", payload, want)
		}
	}
	for _, forbidden := range []string{
		"private query", "query_sha", "result text", `"score"`,
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("delivery leaked %q: %s", forbidden, payload)
		}
	}
}

func TestDocumentRetrievalFailsClosedAtEveryBoundary(t *testing.T) {
	boundaryErr := errors.New("boundary failed")
	tests := []struct {
		name          string
		sourceErr     error
		assignmentErr error
		executionErr  error
		deliveryErr   error
	}{
		{name: "selection", sourceErr: boundaryErr},
		{name: "assignment", assignmentErr: boundaryErr},
		{name: "execution", executionErr: boundaryErr},
		{name: "delivery", deliveryErr: boundaryErr},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := retrievalInput(t)
			control := newDocumentRetrieval(
				retrievalDecisionSourceFunc(func(
					context.Context,
					tools.DocumentRetrievalInput,
				) (toolDecision, error) {
					if test.sourceErr != nil {
						return toolDecision{}, test.sourceErr
					}
					return retrievalShadowDecision(t, input), nil
				}),
				&recordingToolEvents{
					assignmentErr: test.assignmentErr,
					deliveryErr:   test.deliveryErr,
				},
			)
			staticCalls := 0
			static := func(
				context.Context,
				string,
			) (documents.RetrievalResult, error) {
				staticCalls++
				return documents.RetrievalResult{
					PlanID: documents.RetrievalPlanStatic,
					Hits: []documents.SearchHit{{
						DocumentID: "doc-7",
						ChunkID:    "chunk-doc-7-000001",
					}},
					EffectiveLimit: 5,
				}, nil
			}
			execute := func(
				context.Context,
				string,
			) (documents.RetrievalResult, error) {
				if test.executionErr != nil {
					return documents.RetrievalResult{}, test.executionErr
				}
				return static(t.Context(), documents.RetrievalPlanStatic)
			}
			result, err := control.RetrieveDocuments(
				t.Context(), input, execute, static,
			)
			if err != nil || len(result.Hits) != 1 || staticCalls < 1 {
				t.Fatalf(
					"fallback = (%+v, %v), static calls=%d",
					result, err, staticCalls,
				)
			}
		})
	}
}

func retrievalInput(t *testing.T) tools.DocumentRetrievalInput {
	t.Helper()
	service := retrievalTestService()
	frozen, err := service.FreezeRetrievalPlans(documents.SearchRequest{
		Query: "private query", DocumentID: "doc-7",
		IdentityID: uuid.Must(uuid.NewV7()).String(), Limit: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	return tools.DocumentRetrievalInput{
		OwnerID:   frozen.Plans()[0].Scope.OwnerID,
		RequestID: uuid.Must(uuid.NewV7()).String(), PointOrdinal: 7,
		QueryLength: 13, DocumentID: "doc-7", MaxResults: 5,
		PlanIDs: frozen.PlanIDs(), CatalogRevision: frozen.CatalogRevision(),
		Frozen: frozen,
	}
}

func retrievalShadowDecision(
	t *testing.T,
	input tools.DocumentRetrievalInput,
) toolDecision {
	t.Helper()
	actions := make([]adaptive.SnapshotAction, len(input.PlanIDs))
	for index, actionID := range input.PlanIDs {
		actions[index] = adaptive.SnapshotAction{ActionID: actionID}
	}
	snapshot, err := adaptive.NewPolicySnapshot(adaptive.SnapshotSpec{
		Scope: adaptive.SnapshotScope{
			OwnerID: uuid.MustParse(input.OwnerID),
			Domain:  adaptive.DomainKnowledge, Point: adaptive.PointKnowledge,
			ProviderID: "openrouter", ModelID: "production-model",
			PolicyVersion:  "policy-v1",
			TrainingCutoff: time.Now().UTC().Add(-time.Hour),
		},
		ChampionActionID: adaptive.StaticActionID,
		FeatureSchema: []adaptive.SnapshotFeatureDefinition{
			{Key: adaptive.FeatureCandidateCount, Center: 0, Scale: 1},
			{Key: adaptive.FeatureQueryLength, Center: 0, Scale: 1},
			{Key: adaptive.FeatureRetrievalLimit, Center: 0, Scale: 1},
		},
		Actions: actions,
	})
	if err != nil {
		t.Fatal(err)
	}
	return toolDecision{
		Policy: adaptive.Policy{
			Epoch: 1, Version: "policy-v1", Mode: adaptive.PolicyShadow,
		},
		Snapshot: snapshot, Environment: adaptive.EvaluationProductionCanary,
		RecommendedAction: documents.RetrievalPlanVector,
		ProviderID:        "openrouter", ModelID: "production-model",
	}
}

func retrievalTestService() *documents.Service {
	return &documents.Service{
		Searcher: retrievalSearchFunc(func(
			context.Context,
			documents.SearchRequest,
		) ([]documents.SearchHit, error) {
			return nil, nil
		}),
		Knowledge:     retrievalKnowledge{},
		QueryEmbedder: retrievalEmbedder{},
		Reranker:      retrievalReranker{},
		RetrievalRevisions: documents.RetrievalRevisions{
			CorpusEpoch: 9,
			Parser:      "documents-parser-v1", Chunker: "documents-chunker-v1",
			Embedding: "qwen3-embedding-v1", Index: "neo4j-chunk-index-v1",
			Reranker: "aura-rerank-v1", Retriever: "aura-retrieval-plan-v1",
		},
	}
}

type retrievalSearchFunc func(
	context.Context,
	documents.SearchRequest,
) ([]documents.SearchHit, error)

func (fn retrievalSearchFunc) Search(
	ctx context.Context,
	req documents.SearchRequest,
) ([]documents.SearchHit, error) {
	return fn(ctx, req)
}

type retrievalKnowledge struct{}

func (retrievalKnowledge) Read(
	context.Context, string, map[string]any,
) ([]map[string]any, error) {
	return nil, nil
}

func (retrievalKnowledge) Write(
	context.Context, string, map[string]any,
) ([]map[string]any, error) {
	return nil, nil
}

type retrievalEmbedder struct{}

func (retrievalEmbedder) Embed(
	context.Context, []string,
) ([][]float64, error) {
	return [][]float64{{0.1}}, nil
}

type retrievalReranker struct{}

func (retrievalReranker) Rerank(
	context.Context, string, []string,
) ([]rerank.Scored, error) {
	return nil, nil
}
