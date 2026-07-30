package main

import (
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/rerank"
	"github.com/jackc/pgx/v5/pgxpool"
)

// docs_service.go owns every documents.Service composition in cmd/aura. Construction is
// split from connection on purpose: db.Open and knowledge.Open stay in the callers, and
// the three constructors here are pure functions of already-built collaborators. That is
// what lets docs_service_test.go build all three without the stack and compare their
// SHAPES — the drift this file exists to prevent was invisible to behavioural tests,
// because a half-built service does not error, it just quietly does less.

// documentServiceDeps carries the connected collaborators the compositions share.
type documentServiceDeps struct {
	cfg       *config.Config
	pool      *pgxpool.Pool
	graph     documents.KnowledgeClient
	maxBytes  int64
	revisions documents.RetrievalRevisions
}

func (d documentServiceDeps) searcher() *documents.Searcher {
	return &documents.Searcher{Client: d.graph, MUSRIsolation: d.musrIsolation()}
}

func (d documentServiceDeps) musrIsolation() bool {
	return d.cfg != nil && d.cfg.MUSRIsolation
}

// newDocumentIngestService composes the ingest half: job ledger, extractor sidecar,
// sparse indexer, and the durable embedding queue. Service.IngestPath treats a nil
// Embedder as a silent success, so an ingest service that omits it produces a document
// with chunks and no vectors and reports it as searchable.
func newDocumentIngestService(deps documentServiceDeps) *documents.Service {
	return &documents.Service{
		Jobs: documents.NewPostgresJobStore(deps.pool),
		Extractor: &documents.ExtractClient{
			BaseURL: documentsBaseURL(deps.cfg),
			Client:  documentHTTPClient(deps.cfg),
		},
		Indexer:  &documents.Indexer{Client: deps.graph},
		Searcher: deps.searcher(),
		Embedder: runtimeEmbeddingQueue{cfg: deps.cfg, pool: deps.pool},
		MaxBytes: deps.maxBytes,
		// Inert for IngestPath, which never searches — carried so the ingest and
		// retrieval halves cannot disagree about the D-13 path selector.
		MUSRIsolation: deps.musrIsolation(),
	}
}

// newDocumentRetrievalService composes the two-stage retrieval half: sparse searcher,
// graph client for the vector seed and 1-hop expand, query embedder, and the fail-soft
// reranker.
func newDocumentRetrievalService(deps documentServiceDeps) *documents.Service {
	// One-knob local↔cloud rerank swap (D-28): AURA_RERANK_MODEL set → shared
	// OpenRouter endpoint + the single OPENROUTER_API_KEY; unset → local sidecar.
	rerankBase, rerankKey, rerankModel := deps.cfg.RerankRoute()
	return &documents.Service{
		Searcher:      deps.searcher(),
		Knowledge:     deps.graph,
		QueryEmbedder: embeddingClient(deps.cfg, documentHTTPClient(deps.cfg)),
		Reranker: &rerank.RerankClient{
			BaseURL: rerankBase,
			Model:   rerankModel,
			APIKey:  rerankKey,
		},
		// D-13 path selector: on ⇒ the six scoped queries fail closed on foreign/empty
		// identity; off (default) ⇒ the pre-existing unscoped path. Both the Service and
		// its Searcher carry the flag so dense, sparse-fallback, and expand seeds scope
		// together. The caller supplies req.IdentityID.
		MUSRIsolation:      deps.musrIsolation(),
		RetrievalRevisions: deps.revisions,
	}
}

// newDocumentCLIService composes `aura docs`, which both ingests AND retrieves, so it is
// exactly the union of the two runtime halves. Before this it was neither: it set five
// fields of fifteen, so `docs bench` printed an industrial score for a retrieval pipeline
// that was never built. The union is derived rather than re-listed so no field is written
// twice; docs_service_test.go is what keeps it a union, asserting structurally that every
// field a runtime half sets is set here too.
func newDocumentCLIService(deps documentServiceDeps) *documents.Service {
	svc := newDocumentIngestService(deps)
	retrieval := newDocumentRetrievalService(deps)
	svc.Knowledge = retrieval.Knowledge
	svc.QueryEmbedder = retrieval.QueryEmbedder
	svc.Reranker = retrieval.Reranker
	svc.RetrievalRevisions = retrieval.RetrievalRevisions
	return svc
}

// documentMaxBytes is the per-file ingest ceiling. Left at zero, documents.Service falls
// back to DefaultMaxIngestBytes (50 MiB), half the configured runtime ceiling.
func documentMaxBytes(cfg *config.Config) int64 {
	if cfg == nil {
		return 0
	}
	return int64(cfg.AssetMaxDocumentBytes)
}

// defaultRetrievalRevisions is the shipped revision set for the retrieval plan catalog.
// CorpusEpoch is absent because nothing sources one yet — which is why
// FreezeRetrievalPlans currently fails validateRetrievalRevisions on every call. Retrieve
// does not validate revisions, so the path `aura docs` and the document_search fallback
// take is unaffected.
func defaultRetrievalRevisions() documents.RetrievalRevisions {
	return documents.RetrievalRevisions{
		Parser: "documents-parser-v1", Chunker: "documents-chunker-v1",
		Embedding: "qwen3-embedding-v1", Index: "neo4j-chunk-index-v1",
		Reranker: "aura-rerank-v1", Retriever: "aura-retrieval-plan-v1",
	}
}
