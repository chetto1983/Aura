package documents

import (
	"cmp"
	"slices"

	"github.com/chetto1983/aura/internal/rerank"
)

// rrfK is the reciprocal-rank-fusion damping constant for the optional blend mode. 60
// is the canonical RRF constant (Cormack et al. 2009); on Aura's small seed pool
// (N<=15) it keeps a strong seed hit's rank-0 contribution (1/60) large enough that a
// single low-confidence rerank demotion cannot bury it, while a confident rerank can
// still reorder the field.
const rrfK = 60

// applyRerankGuard is the pure, deterministic non-monotonic guard that decides whether
// to trust a rerank reorder. Spike 070 proved rerank is NOT monotonic — the "back to
// box" case demoted the clean answer — so a reorder is trusted only when the reranker
// is confident. seed is the pre-rerank (RRF/vector) order; scored is the reranker
// output (rerank.Scored, sorted by descending relevance, Index pointing into seed);
// threshold is the minimum top score required to trust the reorder.
//
// With blend=false (the default score-threshold mode):
//   - a length mismatch, a single-element pool, an out-of-range index, an identity
//     result, or a top score below threshold all keep the seed order verbatim;
//   - otherwise the reranked order is returned with the rerank scores attached.
//
// With blend=true it returns blendRerankOrders(seed, scored): a stable reciprocal-rank
// fusion of seed rank and rerank rank, so one low-confidence demotion softly blends
// instead of hard-keeping seed — a strong seed hit can never be buried.
//
// It mirrors the search_fusion guarded-tiebreak discipline (apply the secondary signal
// only when confident; keep the primary order otherwise) and performs no I/O.
func applyRerankGuard(seed []SearchHit, scored []rerank.Scored, threshold float64, blend bool) []SearchHit {
	if len(scored) != len(seed) || len(seed) < 2 {
		return seed
	}
	if blend {
		return blendRerankOrders(seed, scored)
	}
	if scored[0].Score < threshold {
		return seed // non-monotonic guard: top score too weak to trust the reorder
	}
	reordered := make([]SearchHit, 0, len(seed))
	changed := false
	for i, sc := range scored {
		if sc.Index < 0 || sc.Index >= len(seed) {
			return seed
		}
		if sc.Index != i {
			changed = true
		}
		hit := seed[sc.Index]
		hit.Score = sc.Score
		reordered = append(reordered, hit)
	}
	if !changed {
		return seed // identity order — keep the seed hits (and their seed scores)
	}
	return reordered
}

// blendRerankOrders fuses the seed order and the rerank order with reciprocal-rank
// fusion (RRF): each hit's blended score is 1/(rrfK+seedRank) + 1/(rrfK+rerankRank). A
// hit that is #1 in the seed but demoted to the rerank tail keeps its large rank-0 seed
// term, so a single low-confidence rerank demotion can never bury a strong seed hit.
// The sort is stable with the seed order as the tie-break, so the result is
// deterministic. An out-of-range index falls back to the seed order verbatim.
func blendRerankOrders(seed []SearchHit, scored []rerank.Scored) []SearchHit {
	rerankRank := make([]int, len(seed))
	for rank, sc := range scored {
		if sc.Index < 0 || sc.Index >= len(seed) {
			return seed
		}
		rerankRank[sc.Index] = rank
	}
	type blended struct {
		hit   SearchHit
		score float64
	}
	items := make([]blended, len(seed))
	for i := range seed {
		items[i] = blended{
			hit:   seed[i],
			score: 1.0/float64(rrfK+i) + 1.0/float64(rrfK+rerankRank[i]),
		}
	}
	// Stable descending sort: the slice starts in seed order, so equal blended scores
	// keep the seed rank as the deterministic tie-break.
	slices.SortStableFunc(items, func(a, b blended) int { return cmp.Compare(b.score, a.score) })
	out := make([]SearchHit, len(items))
	for i, it := range items {
		out[i] = it.hit
	}
	return out
}
