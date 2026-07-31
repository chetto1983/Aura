package documents

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/rerank"
)

func TestFreezeRetrievalPlansFiltersUnhealthyDependencies(t *testing.T) {
	service := &Service{
		Searcher:      &fakeSearchBackend{},
		Knowledge:     &fakeRetrieveKnowledge{},
		QueryEmbedder: &fakeQueryEmbedder{vector: []float64{0.1}},
		RetrievalRevisions: RetrievalRevisions{
			CorpusEpoch: 9,
			Parser:      "documents-parser-v1",
			Chunker:     "documents-chunker-v1",
			Embedding:   "qwen3-embedding-v1",
			Index:       "neo4j-chunk-index-v1",
			Reranker:    "aura-rerank-v1",
			Retriever:   "aura-retrieval-plan-v1",
		},
	}

	frozen, err := service.FreezeRetrievalPlans(SearchRequest{
		Query: "exogenous query", DocumentID: "doc-7",
		IdentityID: "00000000-0000-0000-0000-000000000001", Limit: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		RetrievalPlanSparse,
		RetrievalPlanStatic,
		RetrievalPlanVector,
	}
	if got := frozen.PlanIDs(); !slices.Equal(got, want) {
		t.Fatalf("plan IDs = %v, want %v", got, want)
	}
	for _, plan := range frozen.Plans() {
		if plan.Scope.OwnerID != "00000000-0000-0000-0000-000000000001" ||
			plan.Scope.DocumentID != "doc-7" ||
			plan.Scope.MaxResults != 7 ||
			plan.Revisions != service.RetrievalRevisions {
			t.Fatalf("plan scope/revisions drifted: %+v", plan)
		}
	}
}

func TestFreezeRetrievalPlansAddsOnlyHealthyRerankAndExpand(t *testing.T) {
	service := retrievalPlanTestService()
	frozen, err := service.FreezeRetrievalPlans(SearchRequest{
		Query: "q", IdentityID: "00000000-0000-0000-0000-000000000001",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		RetrievalPlanSparse,
		RetrievalPlanStatic,
		RetrievalPlanVector,
		RetrievalPlanVectorRerank,
		RetrievalPlanVectorRerankExpand,
	}
	if got := frozen.PlanIDs(); !slices.Equal(got, want) {
		t.Fatalf("plan IDs = %v, want %v", got, want)
	}
}

func TestFreezeRetrievalPlansExcludesExpandWhenUnhealthy(t *testing.T) {
	service := retrievalPlanTestService()
	service.RetrievalHealth = &RetrievalDependencyHealth{
		Sparse: true, Vector: true, Reranker: true,
	}
	frozen, err := service.FreezeRetrievalPlans(SearchRequest{
		Query: "q", IdentityID: "00000000-0000-0000-0000-000000000001",
	})
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(frozen.PlanIDs(), RetrievalPlanVectorRerankExpand) {
		t.Fatal("expand plan was offered while expansion was unhealthy")
	}
}

func TestFrozenRetrievalPlanCatalogExcludesQueryAndRejectsScopeDrift(t *testing.T) {
	service := retrievalPlanTestService()
	first, err := service.FreezeRetrievalPlans(SearchRequest{
		Query: "first secret query", DocumentID: "doc-7",
		IdentityID: "00000000-0000-0000-0000-000000000001", Limit: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.FreezeRetrievalPlans(SearchRequest{
		Query: "different secret query", DocumentID: "doc-7",
		IdentityID: "00000000-0000-0000-0000-000000000001", Limit: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.CatalogRevision() != second.CatalogRevision() {
		t.Fatal("catalog revision must not depend on query content")
	}
	if err := first.ValidateRequest(SearchRequest{
		Query: "query may change", DocumentID: "doc-other",
		IdentityID: "00000000-0000-0000-0000-000000000001", Limit: 5,
	}); err == nil {
		t.Fatal("scope drift accepted")
	}
}

func TestFrozenRetrievalPlansValidateExactScopedResult(t *testing.T) {
	service := retrievalPlanTestService()
	service.MUSRIsolation = true
	req := SearchRequest{
		Query: "private query", DocumentID: "doc-7",
		IdentityID: "00000000-0000-0000-0000-000000000001", Limit: 2,
	}
	frozen, err := service.FreezeRetrievalPlans(req)
	if err != nil {
		t.Fatal(err)
	}
	valid := RetrievalResult{
		PlanID: RetrievalPlanVectorRerankExpand,
		Hits: []SearchHit{
			{DocumentID: "doc-7", ChunkID: "chunk-doc-7-000001"},
			{DocumentID: "doc-7", ChunkID: "chunk-doc-7-000002"},
		},
		Context: []SearchHit{{
			DocumentID: "doc-7", ChunkID: "chunk-doc-7-000003",
		}},
		EffectiveLimit: 2,
	}
	if err := frozen.ValidateResult(valid, req); err != nil {
		t.Fatalf("valid result rejected: %v", err)
	}

	tests := []struct {
		name   string
		result RetrievalResult
	}{
		{
			name: "unfrozen plan",
			result: RetrievalResult{
				PlanID: "unknown", EffectiveLimit: 2,
			},
		},
		{
			name: "limit drift",
			result: RetrievalResult{
				PlanID: RetrievalPlanVector, EffectiveLimit: 3,
			},
		},
		{
			name: "context on non-expanding plan",
			result: RetrievalResult{
				PlanID: RetrievalPlanVector, EffectiveLimit: 2,
				Context: []SearchHit{{
					DocumentID: "doc-7", ChunkID: "chunk-doc-7-000003",
				}},
			},
		},
		{
			name: "expansion above cap",
			result: RetrievalResult{
				PlanID: RetrievalPlanVectorRerankExpand, EffectiveLimit: 2,
				Hits: []SearchHit{{
					DocumentID: "doc-7", ChunkID: "chunk-doc-7-000001",
				}},
				Context: scopedChunkHits(
					"doc-7", neighborsPerWinner+1, "context",
				),
			},
		},
		{
			name: "invalid chunk ID",
			result: RetrievalResult{
				PlanID: RetrievalPlanVector, EffectiveLimit: 2,
				Hits: []SearchHit{{
					DocumentID: "doc-7", ChunkID: "INVALID CHUNK",
				}},
			},
		},
		{
			name: "foreign document",
			result: RetrievalResult{
				PlanID: RetrievalPlanVector, EffectiveLimit: 2,
				Hits: []SearchHit{{
					DocumentID: "doc-other", ChunkID: "chunk-doc-other-000001",
				}},
			},
		},
		{
			name: "duplicate chunk",
			result: RetrievalResult{
				PlanID: RetrievalPlanVector, EffectiveLimit: 2,
				Hits: []SearchHit{
					{DocumentID: "doc-7", ChunkID: "chunk-doc-7-000001"},
					{DocumentID: "doc-7", ChunkID: "chunk-doc-7-000001"},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := frozen.ValidateResult(test.result, req); err == nil {
				t.Fatal("invalid result accepted")
			}
		})
	}
}

func TestExecuteRetrievalPlanOwnsEverySupportedRoute(t *testing.T) {
	tests := []struct {
		name          string
		planID        string
		wantHits      string
		wantContext   string
		wantSearch    int
		wantEmbed     int
		wantRerank    int
		wantExpansion bool
		rerankScored  []rerank.Scored
	}{
		{
			name: "sparse", planID: RetrievalPlanSparse,
			wantHits: "sparse-0,sparse-1", wantSearch: 1,
		},
		{
			name: "vector", planID: RetrievalPlanVector,
			wantHits: "vector-0,vector-1", wantEmbed: 1,
		},
		{
			name: "vector rerank", planID: RetrievalPlanVectorRerank,
			wantHits: "vector-1,vector-0", wantEmbed: 1, wantRerank: 1,
		},
		{
			name:     "vector rerank expand",
			planID:   RetrievalPlanVectorRerankExpand,
			wantHits: "vector-1,vector-0", wantContext: "context-0",
			wantEmbed: 1, wantRerank: 1, wantExpansion: true,
		},
		{
			// The static plan is the production route, and it now seeds BOTH legs.
			// It used to seed dense only, with lexical reachable solely as an
			// empty-result fallback, so an exact name was never looked up as a
			// string: live, "ZOPPI SRL" returned seven chunks at cosine .85 and none
			// of them contained ZOPPI, while the fulltext index held the one that did
			// at rank 1. Four seeds reach the reranker here, not two.
			name:     "static fuses the lexical and dense legs",
			planID:   RetrievalPlanStatic,
			wantHits: "vector-0,sparse-0", wantContext: "context-0",
			wantSearch: 1, wantEmbed: 1, wantRerank: 1, wantExpansion: true,
			// Seed order is [sparse-0 vector-0 sparse-1 vector-1]: both legs tie at
			// rank 0, and the tie breaks toward the leg that matched the literal.
			// The reranker then picks across legs, which is the whole point of
			// fusing before it runs.
			rerankScored: []rerank.Scored{
				{Index: 1, Score: 0.99}, {Index: 0, Score: 0.9},
				{Index: 2, Score: 0.5}, {Index: 3, Score: 0.4},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			knowledge := &fakeRetrieveKnowledge{
				seedRows: []map[string]any{
					seedRow("doc-7", "vector-0", "zero", 0.9),
					seedRow("doc-7", "vector-1", "one", 0.8),
				},
				expandRows: []map[string]any{
					seedRow("doc-7", "context-0", "context", 0),
				},
			}
			searcher := &fakeSearchBackend{hits: []SearchHit{
				{DocumentID: "doc-7", ChunkID: "sparse-0"},
				{DocumentID: "doc-7", ChunkID: "sparse-1"},
			}}
			embedder := &fakeQueryEmbedder{vector: []float64{0.1}}
			scored := test.rerankScored
			if scored == nil {
				scored = []rerank.Scored{
					{Index: 1, Score: 0.99}, {Index: 0, Score: 0.8},
				}
			}
			reranker := &fakeReranker{scored: scored}
			service := retrievalPlanTestService()
			service.Searcher = searcher
			service.Knowledge = knowledge
			service.QueryEmbedder = embedder
			service.Reranker = reranker
			req := SearchRequest{
				Query: "q", DocumentID: "doc-7",
				IdentityID: "00000000-0000-0000-0000-000000000001",
				Limit:      2,
			}
			frozen, err := service.FreezeRetrievalPlans(req)
			if err != nil {
				t.Fatal(err)
			}
			result, err := service.ExecuteRetrievalPlan(
				t.Context(), frozen, test.planID, req,
			)
			if err != nil {
				t.Fatal(err)
			}
			if got := chunkIDs(result.Hits); got != test.wantHits {
				t.Fatalf("hits = %s, want %s", got, test.wantHits)
			}
			if got := chunkIDs(result.Context); got != test.wantContext {
				t.Fatalf("context = %s, want %s", got, test.wantContext)
			}
			if len(searcher.requests) != test.wantSearch ||
				embedder.calls != test.wantEmbed ||
				reranker.calls != test.wantRerank ||
				knowledge.readContains("NEXT_CHUNK") != test.wantExpansion {
				t.Fatalf(
					"calls search/embed/rerank/expand = %d/%d/%d/%t",
					len(searcher.requests), embedder.calls, reranker.calls,
					knowledge.readContains("NEXT_CHUNK"),
				)
			}
		})
	}
}

func TestExecuteRetrievalPlanStrictFailureLeavesStaticByteOrderCompatible(t *testing.T) {
	searcher := &fakeSearchBackend{hits: []SearchHit{
		{DocumentID: "doc-7", ChunkID: "sparse-0"},
		{DocumentID: "doc-7", ChunkID: "sparse-1"},
	}}
	service := retrievalPlanTestService()
	service.Searcher = searcher
	service.QueryEmbedder = &fakeQueryEmbedder{err: errors.New("embedding down")}
	req := SearchRequest{
		Query: "q", IdentityID: "00000000-0000-0000-0000-000000000001",
	}
	frozen, err := service.FreezeRetrievalPlans(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ExecuteRetrievalPlan(
		t.Context(), frozen, RetrievalPlanVector, req,
	); err == nil {
		t.Fatal("strict vector plan accepted an embedding failure")
	}
	result, err := service.ExecuteRetrievalPlan(
		t.Context(), frozen, RetrievalPlanStatic, req,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := chunkIDs(result.Flatten()); got != "sparse-0,sparse-1" {
		t.Fatalf("static fallback order = %s", got)
	}
}

func retrievalPlanTestService() *Service {
	return &Service{
		Searcher:      &fakeSearchBackend{},
		Knowledge:     &fakeRetrieveKnowledge{},
		QueryEmbedder: &fakeQueryEmbedder{vector: []float64{0.1}},
		Reranker:      &fakeReranker{},
		RetrievalRevisions: RetrievalRevisions{
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

func scopedChunkHits(documentID string, count int, prefix string) []SearchHit {
	hits := make([]SearchHit, count)
	for index := range count {
		hits[index] = SearchHit{
			DocumentID: documentID,
			ChunkID:    fmt.Sprintf("%s-%06d", prefix, index),
		}
	}
	return hits
}
