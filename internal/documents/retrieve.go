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

// rerankSeeds reorders seeds by rerank relevance. It is fail-soft and honours the
// non-monotonic guard hook (RerankThreshold): a weak top score, an identity result, a
// rerank error, or a length/index mismatch all keep the original seed order.
func (s *Service) rerankSeeds(ctx context.Context, query string, seeds []SearchHit) []SearchHit {
	if s.Reranker == nil || len(seeds) < 2 {
		return seeds
	}
	texts := make([]string, len(seeds))
	for i := range seeds {
		texts[i] = seeds[i].Text
	}
	scored, err := s.Reranker.Rerank(ctx, query, texts)
	if err != nil || len(scored) != len(seeds) {
		return seeds
	}
	if scored[0].Score < s.RerankThreshold {
		return seeds // non-monotonic guard: top score too weak to trust the reorder
	}
	reordered := make([]SearchHit, 0, len(seeds))
	changed := false
	for i, sc := range scored {
		if sc.Index < 0 || sc.Index >= len(seeds) {
			return seeds
		}
		if sc.Index != i {
			changed = true
		}
		hit := seeds[sc.Index]
		hit.Score = sc.Score
		reordered = append(reordered, hit)
	}
	if !changed {
		return seeds // identity order — keep the seed hits (and their seed scores)
	}
	return reordered
}

// expandWinners attaches 1-hop reading-order neighbour context to the reranked
// winners. Only the WINNERS are expanded (not the whole pool); winners stay first in
// reranked order with unique neighbours appended. Expansion is best-effort context:
// any graph error returns the winners unchanged (the answer is already in the seeds).
func (s *Service) expandWinners(ctx context.Context, winners []SearchHit) []SearchHit {
	if s.Knowledge == nil || len(winners) == 0 {
		return winners
	}
	ids := make([]string, len(winners))
	for i := range winners {
		ids[i] = winners[i].ChunkID
	}
	rows, err := s.Knowledge.Read(ctx, neighborExpandQuery, map[string]any{
		"winner_ids":   ids,
		"expand_limit": len(ids) * neighborsPerWinner,
	})
	if err != nil {
		return winners
	}
	neighbors, err := hitsFromRows(rows)
	if err != nil {
		return winners
	}
	out := make([]SearchHit, 0, len(winners)+len(neighbors))
	seen := make(map[string]struct{}, len(winners)+len(neighbors))
	for _, w := range winners {
		out = append(out, w)
		seen[w.ChunkID] = struct{}{}
	}
	for _, n := range neighbors {
		if _, ok := seen[n.ChunkID]; ok {
			continue
		}
		seen[n.ChunkID] = struct{}{}
		out = append(out, n)
	}
	return out
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
