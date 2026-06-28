package documents

import (
	"context"
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
	if s.Reranker == nil {
		// No reranker configured: preserve today's exact sparse-search behaviour.
		return s.Search(ctx, req)
	}
	seeds, err := s.seedHits(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(seeds) == 0 {
		return seeds, nil
	}
	ranked := s.rerankSeeds(ctx, req.Query, seeds)
	winners := topHits(ranked, effectiveLimit(req.Limit))
	return s.expandWinners(ctx, winners), nil
}

// seedHits produces the candidate pool. It prefers the dense vector index when the
// query can be embedded and falls back to the sparse fulltext Search on any embed or
// vector-query failure (NO hard fail) — both paths are the pre-rerank RRF/vector order.
func (s *Service) seedHits(ctx context.Context, req SearchRequest) ([]SearchHit, error) {
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

// vectorSeed runs the dense seed query against the chunk_embedding HNSW index.
func (s *Service) vectorSeed(ctx context.Context, vector []float64, req SearchRequest) ([]SearchHit, error) {
	rows, err := s.Knowledge.Read(ctx, vectorSeedQuery, map[string]any{
		"query_vector":    vector,
		"candidate_limit": seedCandidateLimit,
		"document_id":     req.DocumentID,
	})
	if err != nil {
		return nil, err
	}
	return hitsFromRows(rows)
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

// expandWinners attaches 1-hop reading-order neighbour context to the reranked
// winners as one flat list (winners first in reranked order, unique neighbours
// appended). It is the Retrieve-shaped view of expandNeighbors; GraphRAG keeps the
// winners and neighbours apart instead.
func (s *Service) expandWinners(ctx context.Context, winners []SearchHit) []SearchHit {
	neighbors := s.expandNeighbors(ctx, neighborExpandQuery, winners)
	if len(neighbors) == 0 {
		return winners
	}
	out := make([]SearchHit, 0, len(winners)+len(neighbors))
	out = append(out, winners...)
	out = append(out, neighbors...)
	return out
}

// expandNeighbors fetches the unique 1-hop graph neighbours of the winners via the
// given bounded, $-parameter expansion query, de-duplicated against the winners and
// each other. Only the WINNERS are expanded — never the whole candidate pool — and
// the read is capped at neighborsPerWinner per winner (T-30-11: 1-hop + neighbour
// cap bound the fan-out). Expansion is best-effort context: a missing graph client
// or any graph/decode error yields no neighbours (the answer is in the winners).
func (s *Service) expandNeighbors(ctx context.Context, query string, winners []SearchHit) []SearchHit {
	if s.Knowledge == nil || len(winners) == 0 {
		return nil
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
	})
	if err != nil {
		return nil
	}
	neighbors, err := hitsFromRows(rows)
	if err != nil {
		return nil
	}
	unique := make([]SearchHit, 0, len(neighbors))
	for _, n := range neighbors {
		if _, ok := seen[n.ChunkID]; ok {
			continue
		}
		seen[n.ChunkID] = struct{}{}
		unique = append(unique, n)
	}
	return unique
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

// neighborExpandQuery attaches 1-hop reading-order context: for the reranked winners it
// returns their :NEXT_CHUNK neighbours (both directions), excluding the winners
// themselves, bounded by $expand_limit. :HAS_CHUNK is a Document->Chunk edge (see
// indexer.go), so :NEXT_CHUNK is the chunk-to-chunk reading-order context edge.
const neighborExpandQuery = `
MATCH (c:Chunk) WHERE c.id IN $winner_ids
MATCH (c)-[:NEXT_CHUNK]-(n:Chunk)
WHERE NOT n.id IN $winner_ids
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
