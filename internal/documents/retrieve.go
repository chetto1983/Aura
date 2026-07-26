package documents

import (
	"context"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/rerank"
)

// seedCandidateLimit caps the vector/fulltext seed pool that feeds the reranker.
// Spike 070 Q4 proved reranking the ~10 SEEDS (not the graph-expanded pool) is the
// fast order (267ms vs 1.4s); the pool stays small (N<=15) so rerank cost — which
// scales with pool_size x doc_length — stays bounded.
const seedCandidateLimit = 15

// neighborsPerWinner bounds the 1-hop reading-order expansion per winner (its prev +
// next chunk), so the attached context never floods the result set.
const neighborsPerWinner = 2

// Reranker reorders candidate documents by relevance to a query; it mirrors
// *rerank.RerankClient. It is FAIL-SOFT by contract: on any failure it returns the
// input order (identity) with a nil error, so a missing or GPU-absent sidecar degrades
// retrieval to the seed (RRF/vector) order and never blocks the caller.
type Reranker interface {
	Rerank(ctx context.Context, query string, docs []string) ([]rerank.Scored, error)
}

// Retrieve runs Aura's two-stage retrieval (RET-02): vector/BM25 SEED -> rerank the
// SEEDS -> 1-hop graph-expand the WINNERS for context (spike 070 Q4's fast order, NOT
// expand-then-rerank). It is message-prefix-safe: it reads and writes only chunk rows
// and never references a conversation/system message or the cached prefix.
//
// Fallbacks keep retrieval robust and regression-free: with no Reranker it is exactly
// Search (today's fulltext order); when the query cannot be embedded it seeds from
// fulltext; when the reranker is absent/identity the seed (RRF/vector) order is kept.
func (s *Service) Retrieve(ctx context.Context, req SearchRequest) ([]SearchHit, error) {
	result, err := s.executeRetrievalPlan(ctx, s.staticRetrievalPlan(req), req)
	if err != nil {
		return nil, err
	}
	return result.Flatten(), nil
}

// RetrievalResult keeps exposed winners and expansion context separate while
// sharing one executor between Retrieve and GraphRAG.
type RetrievalResult struct {
	Hits           []SearchHit
	Context        []SearchHit
	Stages         StageTimings
	PlanID         string
	EffectiveLimit int
}

// Flatten returns winners followed by expansion context.
func (result RetrievalResult) Flatten() []SearchHit {
	out := make([]SearchHit, 0, len(result.Hits)+len(result.Context))
	out = append(out, result.Hits...)
	out = append(out, result.Context...)
	return out
}

// ExecuteRetrievalPlan validates the frozen catalog and runs exactly one plan.
func (s *Service) ExecuteRetrievalPlan(
	ctx context.Context,
	frozen FrozenRetrievalPlans,
	planID string,
	req SearchRequest,
) (RetrievalResult, error) {
	if err := frozen.ValidateRequest(req); err != nil {
		return RetrievalResult{}, err
	}
	plan, ok := frozen.Plan(planID)
	if !ok {
		return RetrievalResult{}, fmt.Errorf(
			"documents: retrieval plan %q is not frozen", planID,
		)
	}
	return s.executeRetrievalPlan(ctx, plan, req)
}

func (s *Service) executeRetrievalPlan(
	ctx context.Context,
	plan RetrievalPlan,
	req SearchRequest,
) (RetrievalResult, error) {
	var stages StageTimings
	start := s.nowMono()
	seeds, err := s.executeRetrievalSeed(ctx, plan, req)
	seeded := s.nowMono()
	stages.VectorMS = seeded.Sub(start).Milliseconds()
	if err != nil {
		return RetrievalResult{Stages: stages, PlanID: plan.ID}, err
	}
	if len(seeds) == 0 {
		return RetrievalResult{
			Hits: seeds, Stages: stages, PlanID: plan.ID,
			EffectiveLimit: effectiveLimit(req.Limit),
		}, nil
	}
	ranked, err := s.executeRetrievalRerank(ctx, plan, req.Query, seeds)
	reranked := s.nowMono()
	stages.RerankMS = reranked.Sub(seeded).Milliseconds()
	if err != nil {
		return RetrievalResult{Stages: stages, PlanID: plan.ID}, err
	}
	winners := topHits(ranked, effectiveLimit(req.Limit))
	contextHits, err := s.executeRetrievalExpand(ctx, plan, req, winners)
	expanded := s.nowMono()
	stages.ExpandMS = expanded.Sub(reranked).Milliseconds()
	if err != nil {
		return RetrievalResult{Stages: stages, PlanID: plan.ID}, err
	}
	return RetrievalResult{
		Hits: winners, Context: contextHits, Stages: stages, PlanID: plan.ID,
		EffectiveLimit: effectiveLimit(req.Limit),
	}, nil
}

func (s *Service) executeRetrievalSeed(
	ctx context.Context,
	plan RetrievalPlan,
	req SearchRequest,
) ([]SearchHit, error) {
	if plan.FailSoft {
		switch retrievalSeed(plan.Seed) {
		case retrievalSeedSparse:
			return s.Search(ctx, req)
		case retrievalSeedVector:
			return s.seedHits(ctx, req)
		default:
			return nil, fmt.Errorf(
				"documents: retrieval seed %q is unsupported", plan.Seed,
			)
		}
	}
	switch retrievalSeed(plan.Seed) {
	case retrievalSeedSparse:
		return s.Search(ctx, req)
	case retrievalSeedVector:
		vector, err := s.queryVectorStrict(ctx, req.Query)
		if err != nil {
			return nil, err
		}
		return s.vectorSeed(ctx, vector, req)
	default:
		return nil, fmt.Errorf("documents: retrieval seed %q is unsupported", plan.Seed)
	}
}

func (s *Service) executeRetrievalRerank(
	ctx context.Context,
	plan RetrievalPlan,
	query string,
	seeds []SearchHit,
) ([]SearchHit, error) {
	if !plan.Rerank {
		return seeds, nil
	}
	if plan.FailSoft {
		return s.rerankSeeds(ctx, query, seeds), nil
	}
	if s.Reranker == nil {
		return nil, errors.New("documents: reranker dependency is unavailable")
	}
	if len(seeds) < 2 {
		return seeds, nil
	}
	texts := make([]string, len(seeds))
	for index := range seeds {
		texts[index] = seeds[index].Text
	}
	scored, err := s.Reranker.Rerank(ctx, query, texts)
	if err != nil {
		return nil, fmt.Errorf("documents: rerank retrieval: %w", err)
	}
	return applyRerankGuard(seeds, scored, s.RerankThreshold, s.RerankBlend), nil
}

func (s *Service) executeRetrievalExpand(
	ctx context.Context,
	plan RetrievalPlan,
	req SearchRequest,
	winners []SearchHit,
) ([]SearchHit, error) {
	if !plan.Expand {
		return nil, nil
	}
	query := neighborExpandQuery
	if s.MUSRIsolation {
		query = neighborExpandQueryScoped
	}
	if plan.ID == RetrievalPlanVectorRerankExpand {
		query = graphExpandQuery
		if s.MUSRIsolation {
			query = graphExpandQueryScoped
		}
	}
	if plan.FailSoft {
		return s.expandNeighbors(ctx, query, req.IdentityID, winners), nil
	}
	return s.expandNeighborsStrict(ctx, query, req.IdentityID, winners)
}

func (s *Service) queryVectorStrict(
	ctx context.Context,
	query string,
) ([]float64, error) {
	if s.QueryEmbedder == nil || s.Knowledge == nil {
		return nil, errors.New("documents: vector dependencies are unavailable")
	}
	vectors, err := s.QueryEmbedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("documents: embed retrieval query: %w", err)
	}
	if len(vectors) != 1 || len(vectors[0]) == 0 {
		return nil, errors.New("documents: query embedder returned no vector")
	}
	return vectors[0], nil
}

// seedHits produces the candidate pool. It prefers the dense vector index when the
// query can be embedded and falls back to the sparse fulltext Search on any embed or
// vector-query failure (NO hard fail) — both paths are the pre-rerank RRF/vector order.
func (s *Service) seedHits(ctx context.Context, req SearchRequest) ([]SearchHit, error) {
	// Belt-and-suspenders fail-closed guard (D-09.2): with isolation on, an empty
	// principal owns nothing, so no dense/sparse seed can be reachable — short-circuit
	// before any graph round-trip. The scoped queries enforce the same graph-side.
	if s.MUSRIsolation && req.IdentityID == "" {
		return nil, nil
	}
	if vector, ok := s.queryVector(ctx, req.Query); ok {
		if hits, err := s.vectorSeed(ctx, vector, req); err == nil && len(hits) > 0 {
			return hits, nil
		}
	}
	if s.Searcher == nil {
		return nil, fmt.Errorf("documents: retrieve has no searcher for seed fallback")
	}
	return s.Searcher.Search(ctx, req)
}

// queryVector embeds the query into a single seed vector, reporting ok=false (so the
// caller falls back to fulltext) when no embedder/graph client is wired, or the embed
// fails, or it returns nothing.
func (s *Service) queryVector(ctx context.Context, query string) ([]float64, bool) {
	if s.QueryEmbedder == nil || s.Knowledge == nil {
		return nil, false
	}
	vectors, err := s.QueryEmbedder.Embed(ctx, []string{query})
	if err != nil || len(vectors) != 1 || len(vectors[0]) == 0 {
		return nil, false
	}
	return vectors[0], true
}

// vectorSeed runs the dense seed query. Unscoped it queries the chunk_embedding HNSW index
// (global top-k). Scoped to a document_id it uses a native metadata PRE-filter instead: it
// ranks that document's own chunks by cosine, so a small or freshly-uploaded document is
// never crowded out of the global top-k against a large corpus (spike 075 — the global
// queryNodes(k)-THEN-filter returned 0 for a generic query when the answer was in the doc).
func (s *Service) vectorSeed(ctx context.Context, vector []float64, req SearchRequest) ([]SearchHit, error) {
	query := s.selectVectorSeedQuery(req.DocumentID != "")
	rows, err := s.Knowledge.Read(ctx, query, map[string]any{
		"query_vector":    vector,
		"candidate_limit": seedCandidateLimit,
		"document_id":     req.DocumentID,
		"identity":        req.IdentityID,
	})
	if err != nil {
		return nil, err
	}
	return hitsFromRows(rows)
}

// selectVectorSeedQuery picks the dense seed query across the two orthogonal axes: the
// spike-075 document pre-filter (docScoped when a document_id is supplied) and the Phase-36
// identity isolation flag (the *Scoped variant when MUSRIsolation is on). Isolation on ⇒ the
// variant carrying the unconditional EXISTS ownership filter; off ⇒ the pre-existing query.
func (s *Service) selectVectorSeedQuery(perDocument bool) string {
	switch {
	case perDocument && s.MUSRIsolation:
		return docScopedVectorSeedQueryScoped
	case perDocument:
		return docScopedVectorSeedQuery
	case s.MUSRIsolation:
		return vectorSeedQueryScoped
	default:
		return vectorSeedQuery
	}
}

// rerankSeeds reorders seeds by rerank relevance, then applies the non-monotonic guard
// (applyRerankGuard). It is fail-soft: a missing reranker, a rerank error, a weak top
// score, an identity result, or a length/index mismatch all keep the original seed
// order — the guard owns that decision so Retrieve and GraphRAG share one implementation.
func (s *Service) rerankSeeds(ctx context.Context, query string, seeds []SearchHit) []SearchHit {
	scored, ok := s.rerankScores(ctx, query, seeds)
	if !ok {
		return seeds
	}
	return applyRerankGuard(seeds, scored, s.RerankThreshold, s.RerankBlend)
}

// rerankScores is the I/O half of the two-stage rerank: it runs the reranker over the
// seed texts and returns the scored result. ok is false (and the caller keeps the seed
// order) when no reranker is wired, the pool is too small to reorder, or the rerank
// call errors. Kept apart from the pure applyRerankGuard so both Retrieve and GraphRAG
// reuse one rerank call and one guard (no duplicated threshold logic — CLAUDE.md).
func (s *Service) rerankScores(ctx context.Context, query string, seeds []SearchHit) ([]rerank.Scored, bool) {
	if s.Reranker == nil || len(seeds) < 2 {
		return nil, false
	}
	texts := make([]string, len(seeds))
	for i := range seeds {
		texts[i] = seeds[i].Text
	}
	scored, err := s.Reranker.Rerank(ctx, query, texts)
	if err != nil {
		return nil, false
	}
	return scored, true
}

// expandNeighbors fetches the unique 1-hop graph neighbours of the winners via the
// given bounded, $-parameter expansion query, de-duplicated against the winners and
// each other. Only the WINNERS are expanded — never the whole candidate pool — and
// the read is capped at neighborsPerWinner per winner (T-30-11: 1-hop + neighbour
// cap bound the fan-out). Expansion is best-effort context: a missing graph client
// or any graph/decode error yields no neighbours (the answer is in the winners).
func (s *Service) expandNeighbors(ctx context.Context, query, identity string, winners []SearchHit) []SearchHit {
	neighbors, err := s.expandNeighborsStrict(ctx, query, identity, winners)
	if err != nil {
		return nil
	}
	return neighbors
}

func (s *Service) expandNeighborsStrict(
	ctx context.Context,
	query string,
	identity string,
	winners []SearchHit,
) ([]SearchHit, error) {
	if s.Knowledge == nil || len(winners) == 0 {
		if s.Knowledge == nil && len(winners) > 0 {
			return nil, errors.New("documents: graph expansion is unavailable")
		}
		return nil, nil
	}
	ids := make([]string, len(winners))
	seen := make(map[string]struct{}, len(winners))
	for i := range winners {
		ids[i] = winners[i].ChunkID
		seen[winners[i].ChunkID] = struct{}{}
	}
	rows, err := s.Knowledge.Read(ctx, query, map[string]any{
		"winner_ids":   ids,
		"expand_limit": len(ids) * neighborsPerWinner,
		"identity":     identity,
	})
	if err != nil {
		return nil, err
	}
	neighbors, err := hitsFromRows(rows)
	if err != nil {
		return nil, err
	}
	unique := make([]SearchHit, 0, len(neighbors))
	for _, n := range neighbors {
		if _, ok := seen[n.ChunkID]; ok {
			continue
		}
		seen[n.ChunkID] = struct{}{}
		unique = append(unique, n)
	}
	return unique, nil
}

// topHits keeps the first k hits (the reranked winners); k<=0 keeps all.
func topHits(hits []SearchHit, k int) []SearchHit {
	if k <= 0 || len(hits) <= k {
		return hits
	}
	return hits[:k]
}

// effectiveLimit clamps a requested limit into [defaultSearchLimit, maxSearchLimit].
func effectiveLimit(limit int) int {
	if limit <= 0 {
		return defaultSearchLimit
	}
	if limit > maxSearchLimit {
		return maxSearchLimit
	}
	return limit
}

// vectorSeedQuery seeds candidates from the dense chunk_embedding HNSW index, mirroring
// sparseSearchQuery's projection so reranking and expansion operate on identical rows.
const vectorSeedQuery = `
CALL db.index.vector.queryNodes('chunk_embedding', $candidate_limit, $query_vector)
YIELD node, score
WHERE ($document_id = "" OR node.document_id = $document_id)
  AND coalesce(node.active, true) = true
  AND node.deleted_at IS NULL
RETURN
  node.document_id AS document_id,
  coalesce(node.file_name, "") AS file_name,
  node.id AS chunk_id,
  node.text AS text,
  node.locator_json AS locator_json,
  node.heading_path AS heading_path,
  score AS score
ORDER BY score DESC
`

// vectorSeedQueryScoped is the identity-scoped variant of vectorSeedQuery (Phase 36 D-13).
// The HNSW index can't be node-restricted, so the ownership predicate is post-YIELD: the
// UNCONDITIONAL `$identity <> "" AND EXISTS {...}` fails closed on an empty identity and
// drops any chunk whose document the caller does not own. No `= "" OR` fallthrough (Pitfall
// 5). Projection matches vectorSeedQuery so the shared hitsFromRows decodes both.
const vectorSeedQueryScoped = `
CALL db.index.vector.queryNodes('chunk_embedding', $candidate_limit, $query_vector)
YIELD node, score
WHERE ($document_id = "" OR node.document_id = $document_id)
  AND coalesce(node.active, true) = true
  AND node.deleted_at IS NULL
  AND $identity <> ""
  AND EXISTS { (:User {identifier: $identity})-[:HAS_DOCUMENT]->(:Document {id: node.document_id}) }
RETURN
  node.document_id AS document_id,
  coalesce(node.file_name, "") AS file_name,
  node.id AS chunk_id,
  node.text AS text,
  node.locator_json AS locator_json,
  node.heading_path AS heading_path,
  score AS score
ORDER BY score DESC
`

// docScopedVectorSeedQuery is the scoped-retrieval seed: with a document_id supplied it
// ranks that document's OWN chunks by cosine (a native metadata pre-filter via the indexed
// document_id property), so a small or freshly-uploaded document is never crowded out of the
// global vector top-k (spike 075). A document has few chunks, so the per-chunk cosine is
// exact and cheap. The projection matches vectorSeedQuery so hitsFromRows and the
// rerank/expand stages are identical. Chunks without an embedding yet (async-embed in
// flight) are skipped; seedHits then falls back to the sparse fulltext seed.
const docScopedVectorSeedQuery = `
MATCH (node:Chunk {document_id: $document_id})
WHERE node.embedding IS NOT NULL
  AND coalesce(node.active, true) = true
  AND node.deleted_at IS NULL
WITH node, vector.similarity.cosine(node.embedding, $query_vector) AS score
RETURN
  node.document_id AS document_id,
  coalesce(node.file_name, "") AS file_name,
  node.id AS chunk_id,
  node.text AS text,
  node.locator_json AS locator_json,
  node.heading_path AS heading_path,
  score AS score
ORDER BY score DESC
LIMIT $candidate_limit
`

// docScopedVectorSeedQueryScoped is the identity-scoped variant of docScopedVectorSeedQuery
// (Phase 36 D-13). A MATCH pre-filter, so the UNCONDITIONAL ownership predicate rides the
// initial WHERE: empty identity fails closed via `$identity <> ""`, foreign identity dropped
// by the EXISTS. No `= "" OR` fallthrough (Pitfall 5).
const docScopedVectorSeedQueryScoped = `
MATCH (node:Chunk {document_id: $document_id})
WHERE node.embedding IS NOT NULL
  AND coalesce(node.active, true) = true
  AND node.deleted_at IS NULL
  AND $identity <> ""
  AND EXISTS { (:User {identifier: $identity})-[:HAS_DOCUMENT]->(:Document {id: node.document_id}) }
WITH node, vector.similarity.cosine(node.embedding, $query_vector) AS score
RETURN
  node.document_id AS document_id,
  coalesce(node.file_name, "") AS file_name,
  node.id AS chunk_id,
  node.text AS text,
  node.locator_json AS locator_json,
  node.heading_path AS heading_path,
  score AS score
ORDER BY score DESC
LIMIT $candidate_limit
`

// neighborExpandQuery attaches 1-hop reading-order context: for the reranked winners it
// returns their :NEXT_CHUNK neighbours (both directions), excluding the winners
// themselves, bounded by $expand_limit. :HAS_CHUNK is a Document->Chunk edge (see
// indexer.go), so :NEXT_CHUNK is the chunk-to-chunk reading-order context edge.
const neighborExpandQuery = `
MATCH (c:Chunk) WHERE c.id IN $winner_ids
MATCH (c)-[:NEXT_CHUNK]-(n:Chunk)
WHERE NOT n.id IN $winner_ids
  AND coalesce(n.active, true) = true
  AND n.deleted_at IS NULL
WITH DISTINCT n
RETURN
  n.document_id AS document_id,
  coalesce(n.file_name, "") AS file_name,
  n.id AS chunk_id,
  n.text AS text,
  n.locator_json AS locator_json,
  n.heading_path AS heading_path,
  0.0 AS score
LIMIT $expand_limit
`

// neighborExpandQueryScoped is the identity-scoped variant of neighborExpandQuery (Phase 36
// D-13, defense-in-depth on the 1-hop expand). The neighbour n is kept only when the caller
// owns n's document: the UNCONDITIONAL `$identity <> "" AND EXISTS {...}` fails closed on an
// empty identity and never attaches a foreign-owned neighbour as context. No `= "" OR`
// fallthrough (Pitfall 5).
const neighborExpandQueryScoped = `
MATCH (c:Chunk) WHERE c.id IN $winner_ids
MATCH (c)-[:NEXT_CHUNK]-(n:Chunk)
WHERE NOT n.id IN $winner_ids
  AND coalesce(n.active, true) = true
  AND n.deleted_at IS NULL
  AND $identity <> ""
  AND EXISTS { (:User {identifier: $identity})-[:HAS_DOCUMENT]->(:Document {id: n.document_id}) }
WITH DISTINCT n
RETURN
  n.document_id AS document_id,
  coalesce(n.file_name, "") AS file_name,
  n.id AS chunk_id,
  n.text AS text,
  n.locator_json AS locator_json,
  n.heading_path AS heading_path,
  0.0 AS score
LIMIT $expand_limit
`
