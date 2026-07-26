package documents

import (
	"context"
	"time"
)

// GraphRAGResult is Aura's connected-nodes retrieval result (RET-04): the reranked
// winner chunks (the answer, in reranked order), their 1-hop connected neighbours as
// context, and the per-stage latency. Winners and context are kept apart so a caller
// can cite the answer chunks and present the surrounding reading-order context
// distinctly.
type GraphRAGResult struct {
	Hits    []SearchHit  `json:"hits"`
	Context []SearchHit  `json:"context,omitempty"`
	Stages  StageTimings `json:"stages"`
}

// StageTimings records each retrieval stage's wall-clock latency in milliseconds so a
// per-stage p95 budget is observable. Spike 070 Q2 proved the vector seed and graph
// expand are trivially cheap (~tens of ms) while rerank is the dominant bounded cost;
// surfacing the three separately lets RET-05's perf gate assert the budget and catch a
// rerank regression. A zero is valid only for a stage skipped by a fallback (RerankMS
// and ExpandMS are 0 when the seed pool is empty; RerankMS is ~0 when no reranker is
// wired).
type StageTimings struct {
	VectorMS int64 `json:"vector_ms"`
	ExpandMS int64 `json:"expand_ms"`
	RerankMS int64 `json:"rerank_ms"`
}

// GraphRAG runs connected-nodes retrieval in the spike-070 Q4 fast order — vector/BM25
// SEED -> rerank the SEEDS -> 1-hop graph-expand the WINNERS for context — and times
// each stage so the per-stage p95 budget is provable. The reranked winners (the
// answer) stay in Hits in reranked order; their 1-hop :NEXT_CHUNK/:HAS_CHUNK
// neighbours ride in Context.
//
// Every stage is fail-soft and the order is fixed seed->rerank->expand (candidates are
// NEVER re-seeded from the expanded pool; expansion adds context around winners only):
//   - an unembeddable query seeds from fulltext (timing still recorded under VectorMS,
//     the seed stage), with no hard fail;
//   - a missing/identity/below-threshold reranker keeps the seed order (RerankMS still
//     recorded) — rerank failure NEVER blocks GraphRAG;
//   - a graph error returns the winners with empty Context.
//
// It is message-prefix-safe: it reads and writes only chunk rows, never the cached
// system/conversation prefix.
func (s *Service) GraphRAG(ctx context.Context, req SearchRequest) (GraphRAGResult, error) {
	plan := s.staticRetrievalPlan(req)
	plan.ID = RetrievalPlanVectorRerankExpand
	plan.Revision = "static-graphrag-v1"
	plan.Seed = string(retrievalSeedVector)
	plan.Expand = s.Knowledge != nil
	result, err := s.executeRetrievalPlan(ctx, plan, req)
	return GraphRAGResult{
		Hits: result.Hits, Context: result.Context, Stages: result.Stages,
	}, err
}

// nowMono returns the monotonic clock GraphRAG times stages with: time.Now in
// production (its monotonic reading makes Sub immune to wall-clock jumps), or the
// injected timeSource in tests for deterministic stage durations.
func (s *Service) nowMono() time.Time {
	if s.timeSource != nil {
		return s.timeSource()
	}
	return time.Now()
}

// graphExpandQuery attaches 1-hop CONNECTED-NODE context to the reranked winners. It
// traverses the connected-document relationship set — the :NEXT_CHUNK reading-order
// chain and the :HAS_CHUNK membership edge — undirected and 1-hop, to the winners'
// neighbouring chunks, excluding the winners themselves and bounded by $expand_limit
// (T-30-11: 1-hop + neighbour cap keep the fan-out from exploding). All inputs ride
// bound $-parameters — never string-interpolated into the body (T-30-12). It mirrors
// neighborExpandQuery's projection so rows decode through the shared hitsFromRows.
//
// Per the live graph model (indexer.go), :HAS_CHUNK is a Document->Chunk edge, so the
// chunk-anchored :HAS_CHUNK arm of the union matches no sibling chunk today; the
// reading-order :NEXT_CHUNK edge supplies the 1-hop context. :HAS_CHUNK is retained in
// the connected-document set the plan specifies and stays bounded by the same cap.
const graphExpandQuery = `
MATCH (c:Chunk) WHERE c.id IN $winner_ids
MATCH (c)-[:NEXT_CHUNK|HAS_CHUNK]-(n:Chunk)
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

// graphExpandQueryScoped is the identity-scoped variant of graphExpandQuery (Phase 36 D-13,
// defense-in-depth on the connected-node expand). A neighbour n survives only when the caller
// owns n's document via the UNCONDITIONAL `$identity <> "" AND EXISTS {...}` — an empty
// identity fails closed and a foreign-owned connected chunk is never surfaced as context. No
// `= "" OR` fallthrough (Pitfall 5).
const graphExpandQueryScoped = `
MATCH (c:Chunk) WHERE c.id IN $winner_ids
MATCH (c)-[:NEXT_CHUNK|HAS_CHUNK]-(n:Chunk)
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
