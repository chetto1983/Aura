package main

import (
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/knowledge"
)

func TestDocsToolSearcherFailsClosedWithoutCorpusEpoch(t *testing.T) {
	searcher := docsToolSearcher{cfg: &config.Config{}}
	_, err := searcher.FreezeRetrievalPlans(documents.SearchRequest{
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
		cfg: &config.Config{
			Neo4j: knowledge.Config{
				BoltURL:  "bolt://neo4j.test:7687",
				EmbedURL: "http://embed.test",
			},
			RerankBaseURL: "http://rerank.test",
		},
		retrievalRevisions: &revisions,
	}
	frozen, err := searcher.FreezeRetrievalPlans(documents.SearchRequest{
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
