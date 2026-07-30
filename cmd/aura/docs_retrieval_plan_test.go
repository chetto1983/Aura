package main

import (
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/knowledge"
)

func TestDocsToolSearcherFailsClosedWithoutCorpusEpoch(t *testing.T) {
	searcher := docsToolSearcher{
		cfg:   configuredDocumentRetrievalTestConfig(),
		graph: &recordingDocumentKnowledgeClient{},
	}
	_, err := searcher.FreezeRetrievalPlans(t.Context(), documents.SearchRequest{
		Query:      "q",
		IdentityID: "00000000-0000-0000-0000-000000000001",
	})
	if err == nil {
		t.Fatal("live plan catalog accepted a missing corpus epoch")
	}
}

func TestDocsToolSearcherFreezesConfiguredImmutableRevisions(t *testing.T) {
	revisions := documents.RetrievalRevisions{
		CorpusEpoch: 9,
		Parser:      "documents-parser-v1", Chunker: "documents-chunker-v1",
		Embedding: "qwen3-embedding-v1", Index: "neo4j-chunk-index-v1",
		Reranker: "aura-rerank-v1", Retriever: "aura-retrieval-plan-v1",
	}
	searcher := docsToolSearcher{
		cfg:                configuredDocumentRetrievalTestConfig(),
		graph:              &recordingDocumentKnowledgeClient{epochReads: []any{int64(9)}},
		retrievalRevisions: &revisions,
	}
	frozen, err := searcher.FreezeRetrievalPlans(t.Context(), documents.SearchRequest{
		Query: "q", DocumentID: "doc-7",
		IdentityID: "00000000-0000-0000-0000-000000000001", Limit: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, plan := range frozen.Plans() {
		if plan.Revisions != revisions {
			t.Fatalf("plan revisions = %+v", plan.Revisions)
		}
	}
}

func TestDocsToolSearcherRejectsCorpusDriftAroundExecution(t *testing.T) {
	for name, epochs := range map[string][]any{
		"before execution": {int64(9), int64(10)},
		"while executing":  {int64(9), int64(9), int64(10)},
	} {
		t.Run(name, func(t *testing.T) {
			searcher := docsToolSearcher{
				cfg:   configuredDocumentRetrievalTestConfig(),
				graph: &recordingDocumentKnowledgeClient{epochReads: epochs},
			}
			req := documents.SearchRequest{
				Query: "q", IdentityID: "00000000-0000-0000-0000-000000000001",
			}
			frozen, err := searcher.FreezeRetrievalPlans(t.Context(), req)
			if err != nil {
				t.Fatal(err)
			}
			_, err = searcher.ExecuteRetrievalPlan(
				t.Context(), frozen, documents.RetrievalPlanSparse, req,
			)
			if err == nil {
				t.Fatal("corpus drift was accepted")
			}
		})
	}
}

func configuredDocumentRetrievalTestConfig() *config.Config {
	return &config.Config{
		Neo4j: knowledge.Config{
			BoltURL:  "bolt://neo4j.test:7687",
			EmbedURL: "http://embed.test",
		},
		RerankBaseURL: "http://rerank.test",
	}
}
