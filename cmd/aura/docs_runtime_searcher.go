package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/knowledge"
)

type documentKnowledgeClient interface {
	documents.KnowledgeClient
	Close() error
}

type documentKnowledgeOpener func(
	context.Context,
	*knowledge.Config,
) (documentKnowledgeClient, error)

type docsToolSearcher struct {
	cfg                *config.Config
	retrievalRevisions *documents.RetrievalRevisions
	openKnowledge      documentKnowledgeOpener

	mu    sync.Mutex
	graph documentKnowledgeClient
}

// Retrieve runs the two-stage retrieval pipeline for the document_search tool. It
// seeds from the dense chunk_embedding index (embed sidecar) or the sparse fulltext
// index, reranks the seeds via the optional aura-rerank sidecar, and 1-hop expands the
// winners. Every stage is fail-soft: a down embed/rerank sidecar degrades to the
// RRF/vector seed order, so retrieval never blocks on optional infrastructure.
func (s *docsToolSearcher) Retrieve(
	ctx context.Context,
	req documents.SearchRequest,
) ([]documents.SearchHit, error) {
	service, err := s.retrievalService(ctx)
	if err != nil {
		return nil, err
	}
	return service.Retrieve(ctx, req)
}

func (s *docsToolSearcher) FreezeRetrievalPlans(
	ctx context.Context,
	req documents.SearchRequest,
) (documents.FrozenRetrievalPlans, error) {
	if s == nil || s.cfg == nil {
		return documents.FrozenRetrievalPlans{},
			fmt.Errorf("document search config is nil")
	}
	revisions, err := s.currentRetrievalRevisions(ctx)
	if err != nil {
		return documents.FrozenRetrievalPlans{}, err
	}
	health := configuredRetrievalHealth(s.cfg)
	service := &documents.Service{
		RetrievalRevisions: revisions,
		RetrievalHealth:    &health,
		MUSRIsolation:      s.cfg.MUSRIsolation,
	}
	return service.FreezeRetrievalPlans(req)
}

func (s *docsToolSearcher) ExecuteRetrievalPlan(
	ctx context.Context,
	frozen documents.FrozenRetrievalPlans,
	planID string,
	req documents.SearchRequest,
) (documents.RetrievalResult, error) {
	if err := s.verifyFrozenCorpusEpoch(ctx, frozen); err != nil {
		return documents.RetrievalResult{}, err
	}
	service, err := s.retrievalService(ctx)
	if err != nil {
		return documents.RetrievalResult{}, err
	}
	result, err := service.ExecuteRetrievalPlan(ctx, frozen, planID, req)
	if err != nil {
		return documents.RetrievalResult{}, err
	}
	if err := s.verifyFrozenCorpusEpoch(ctx, frozen); err != nil {
		return documents.RetrievalResult{}, err
	}
	return result, nil
}

func (s *docsToolSearcher) retrievalService(
	ctx context.Context,
) (*documents.Service, error) {
	if s == nil || s.cfg == nil {
		return nil, fmt.Errorf("document search config is nil")
	}
	graph, err := s.graphClient(ctx)
	if err != nil {
		return nil, err
	}
	return newDocumentRetrievalService(documentServiceDeps{
		cfg: s.cfg, graph: graph, revisions: s.frozenRetrievalRevisions(),
	}), nil
}

func (s *docsToolSearcher) graphClient(
	ctx context.Context,
) (documentKnowledgeClient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.graph != nil {
		return s.graph, nil
	}
	open := s.openKnowledge
	if open == nil {
		open = openDocumentKnowledge
	}
	graph, err := open(ctx, &s.cfg.Neo4j)
	if err != nil {
		return nil, err
	}
	s.graph = graph
	return graph, nil
}

func openDocumentKnowledge(
	ctx context.Context,
	cfg *knowledge.Config,
) (documentKnowledgeClient, error) {
	return knowledge.Open(ctx, cfg)
}

func (s *docsToolSearcher) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.graph == nil {
		return nil
	}
	graph := s.graph
	s.graph = nil
	return graph.Close()
}

func (s *docsToolSearcher) frozenRetrievalRevisions() documents.RetrievalRevisions {
	if s.retrievalRevisions != nil {
		return *s.retrievalRevisions
	}
	return defaultRetrievalRevisions()
}

func (s *docsToolSearcher) currentRetrievalRevisions(
	ctx context.Context,
) (documents.RetrievalRevisions, error) {
	graph, err := s.graphClient(ctx)
	if err != nil {
		return documents.RetrievalRevisions{}, err
	}
	epoch, err := documents.ReadDocumentCorpusEpoch(ctx, graph)
	if err != nil {
		return documents.RetrievalRevisions{}, err
	}
	revisions := s.frozenRetrievalRevisions()
	revisions.CorpusEpoch = epoch
	return revisions, nil
}

func (s *docsToolSearcher) verifyFrozenCorpusEpoch(
	ctx context.Context,
	frozen documents.FrozenRetrievalPlans,
) error {
	want := frozen.CorpusEpoch()
	if want == 0 {
		return fmt.Errorf("document retrieval plan has no corpus epoch")
	}
	graph, err := s.graphClient(ctx)
	if err != nil {
		return err
	}
	got, err := documents.ReadDocumentCorpusEpoch(ctx, graph)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf(
			"document corpus changed during retrieval: epoch %d, frozen %d",
			got,
			want,
		)
	}
	return nil
}

func configuredRetrievalHealth(
	cfg *config.Config,
) documents.RetrievalDependencyHealth {
	if cfg == nil {
		return documents.RetrievalDependencyHealth{}
	}
	rerankBase, _, _ := cfg.RerankRoute()
	return documents.RetrievalDependencyHealth{
		Sparse: cfg.Neo4j.BoltURL != "",
		Vector: cfg.Neo4j.BoltURL != "" && cfg.Neo4j.EmbedURL != "",
		Reranker: cfg.Neo4j.BoltURL != "" && cfg.Neo4j.EmbedURL != "" &&
			rerankBase != "",
		Expand: cfg.Neo4j.BoltURL != "",
	}
}
