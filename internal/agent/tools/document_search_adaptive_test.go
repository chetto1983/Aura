package tools

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/rerank"
)

func TestDocumentSearchAdaptiveOrderIsFreezeAssignExecuteDeliverReturn(t *testing.T) {
	var events []string
	backend := adaptiveDocumentBackend{events: &events}
	control := documentRetrievalControlFunc(func(
		ctx context.Context,
		input DocumentRetrievalInput,
		execute DocumentRetrievalExecutor,
		static DocumentRetrievalExecutor,
	) (documents.RetrievalResult, error) {
		events = append(events, "assignment")
		if input.QueryLength != len("private query") ||
			input.RequestID != "22222222-2222-7222-8222-222222222222" {
			t.Fatalf("privacy-safe input = %+v", input)
		}
		result, err := execute(ctx, documents.RetrievalPlanVectorRerankExpand)
		if err != nil {
			return documents.RetrievalResult{}, err
		}
		events = append(events, "delivery")
		return result, nil
	})
	tool := &DocumentSearch{Searcher: backend, Adaptive: control}
	ctx := WithRequestID(
		toolTestContext(t),
		"22222222-2222-7222-8222-222222222222",
	)
	result, err := tool.Execute(
		ctx,
		json.RawMessage(`{"query":"private query","document_id":"doc-7","limit":2}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	events = append(events, "return")
	if want := []string{"freeze", "assignment", "io", "delivery", "return"}; !slices.Equal(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	if result.Preview == "" {
		t.Fatal("document result was not exposed")
	}
}

func TestDocumentSearchAdaptiveFailureReturnsExactStaticResult(t *testing.T) {
	backend := adaptiveDocumentBackend{}
	control := documentRetrievalControlFunc(func(
		ctx context.Context,
		_ DocumentRetrievalInput,
		_ DocumentRetrievalExecutor,
		static DocumentRetrievalExecutor,
	) (documents.RetrievalResult, error) {
		return static(ctx, documents.RetrievalPlanStatic)
	})
	tool := &DocumentSearch{Searcher: backend, Adaptive: control}
	result, err := tool.Execute(
		WithRequestID(
			toolTestContext(t),
			"22222222-2222-7222-8222-222222222222",
		),
		json.RawMessage(`{"query":"private query","document_id":"doc-7","limit":2}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Preview == "" {
		t.Fatal("static result missing")
	}
}

func TestDocumentSearchRejectsUnfrozenAdaptiveResult(t *testing.T) {
	backend := adaptiveDocumentBackend{}
	control := documentRetrievalControlFunc(func(
		context.Context,
		DocumentRetrievalInput,
		DocumentRetrievalExecutor,
		DocumentRetrievalExecutor,
	) (documents.RetrievalResult, error) {
		return documents.RetrievalResult{
			PlanID: documents.RetrievalPlanVector,
			Hits: []documents.SearchHit{{
				DocumentID: "foreign-doc", ChunkID: "chunk-foreign",
			}},
			EffectiveLimit: 2,
		}, nil
	})
	tool := &DocumentSearch{Searcher: backend, Adaptive: control}
	result, err := tool.Execute(
		WithRequestID(
			toolTestContext(t),
			"22222222-2222-7222-8222-222222222222",
		),
		json.RawMessage(`{"query":"private query","document_id":"doc-7","limit":2}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Preview == "" {
		t.Fatal("static fallback missing")
	}
}

type documentRetrievalControlFunc func(
	context.Context,
	DocumentRetrievalInput,
	DocumentRetrievalExecutor,
	DocumentRetrievalExecutor,
) (documents.RetrievalResult, error)

func (fn documentRetrievalControlFunc) RetrieveDocuments(
	ctx context.Context,
	input DocumentRetrievalInput,
	execute DocumentRetrievalExecutor,
	static DocumentRetrievalExecutor,
) (documents.RetrievalResult, error) {
	return fn(ctx, input, execute, static)
}

type adaptiveDocumentBackend struct {
	events *[]string
}

func (backend adaptiveDocumentBackend) Retrieve(
	_ context.Context,
	_ documents.SearchRequest,
) ([]documents.SearchHit, error) {
	return nil, errors.New("legacy Retrieve must not run on the adaptive path")
}

func (backend adaptiveDocumentBackend) FreezeRetrievalPlans(
	req documents.SearchRequest,
) (documents.FrozenRetrievalPlans, error) {
	if backend.events != nil {
		*backend.events = append(*backend.events, "freeze")
	}
	service := adaptiveDocumentService()
	return service.FreezeRetrievalPlans(req)
}

func (backend adaptiveDocumentBackend) ExecuteRetrievalPlan(
	ctx context.Context,
	frozen documents.FrozenRetrievalPlans,
	planID string,
	req documents.SearchRequest,
) (documents.RetrievalResult, error) {
	if backend.events != nil {
		*backend.events = append(*backend.events, "io")
	}
	service := adaptiveDocumentService()
	return service.ExecuteRetrievalPlan(ctx, frozen, planID, req)
}

func adaptiveDocumentService() *documents.Service {
	return &documents.Service{
		Searcher: documentSearchBackendFunc(func(
			context.Context,
			documents.SearchRequest,
		) ([]documents.SearchHit, error) {
			return []documents.SearchHit{{
				DocumentID: "doc-7", ChunkID: "chunk-doc-7-000001",
			}}, nil
		}),
		Knowledge:     &adaptiveDocumentKnowledge{},
		QueryEmbedder: adaptiveDocumentEmbedder{},
		Reranker:      adaptiveDocumentReranker{},
		RetrievalRevisions: documents.RetrievalRevisions{
			CorpusEpoch: 9,
			Parser:      "documents-parser-v1",
			Chunker:     "documents-chunker-v1",
			Embedding:   "qwen3-embedding-v1",
			Index:       "neo4j-chunk-index-v1",
			Reranker:    "aura-rerank-v1",
			Retriever:   "aura-retrieval-plan-v1",
		},
	}
}

type documentSearchBackendFunc func(
	context.Context,
	documents.SearchRequest,
) ([]documents.SearchHit, error)

func (fn documentSearchBackendFunc) Search(
	ctx context.Context,
	req documents.SearchRequest,
) ([]documents.SearchHit, error) {
	return fn(ctx, req)
}

type adaptiveDocumentEmbedder struct{}

func (adaptiveDocumentEmbedder) Embed(
	context.Context,
	[]string,
) ([][]float64, error) {
	return [][]float64{{0.1}}, nil
}

type adaptiveDocumentReranker struct{}

func (adaptiveDocumentReranker) Rerank(
	_ context.Context,
	_ string,
	_ []string,
) ([]rerank.Scored, error) {
	return nil, nil
}

type adaptiveDocumentKnowledge struct{}

func (*adaptiveDocumentKnowledge) Read(
	context.Context,
	string,
	map[string]any,
) ([]map[string]any, error) {
	return []map[string]any{{
		"document_id": "doc-7", "chunk_id": "chunk-doc-7-000001",
		"file_name": "manual.pdf", "text": "result", "score": 1.0,
		"locator_json": "", "heading_path": []string{},
	}}, nil
}

func (*adaptiveDocumentKnowledge) Write(
	context.Context,
	string,
	map[string]any,
) ([]map[string]any, error) {
	return nil, nil
}
