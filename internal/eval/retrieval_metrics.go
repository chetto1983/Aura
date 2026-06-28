// retrieval_metrics.go holds Aura's pure information-retrieval metrics — nDCG@10,
// Recall@5, and MRR — used by the retrieval_eval harness (RET-05) to quantify the
// rerank lift over a judged query set. It carries NO build tag so the metrics compile
// and are unit-tested (retrieval_metrics_test.go) under the default build, keeping them
// coverage-counted by the owned-surface floor. The functions are pure: they operate on
// a ranked id list and a relevant-id set, with no I/O and no global state.
package eval

import "math"

// relevantSet builds a membership set from a slice of relevant ids, de-duplicating so a
// repeated judgment id is counted once in the denominator of Recall@k.
func relevantSet(ids []string) map[string]struct{} {
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return set
}

// ndcgAtK is the normalized discounted cumulative gain at rank k for binary relevance:
// DCG@k / IDCG@k, where DCG sums 1/log2(rank+1) over the relevant ids in the top k and
// IDCG is the DCG of the ideal ranking (all relevant ids first, capped at k). It returns
// 0 when k<=0, the relevant set is empty, or the ideal gain is zero. The result is in
// [0,1]: 1.0 for a ranking that front-loads all relevant ids.
func ndcgAtK(ranked []string, relevant map[string]struct{}, k int) float64 {
	if k <= 0 || len(relevant) == 0 {
		return 0
	}
	dcg := 0.0
	for i := 0; i < min(k, len(ranked)); i++ {
		if _, ok := relevant[ranked[i]]; ok {
			dcg += 1.0 / math.Log2(float64(i+2))
		}
	}
	idcg := 0.0
	for i := 0; i < min(k, len(relevant)); i++ {
		idcg += 1.0 / math.Log2(float64(i+2))
	}
	if idcg == 0 {
		return 0
	}
	return dcg / idcg
}

// recallAtK is the fraction of the relevant ids that appear in the top k of the ranked
// list: |relevant ∩ ranked[:k]| / |relevant|. It de-duplicates the ranked list so a
// repeated hit cannot inflate the numerator, and returns 0 when k<=0 or the relevant
// set is empty.
func recallAtK(ranked []string, relevant map[string]struct{}, k int) float64 {
	if k <= 0 || len(relevant) == 0 {
		return 0
	}
	found := make(map[string]struct{}, len(relevant))
	for i := 0; i < min(k, len(ranked)); i++ {
		id := ranked[i]
		if _, ok := relevant[id]; ok {
			found[id] = struct{}{}
		}
	}
	return float64(len(found)) / float64(len(relevant))
}

// mrr is the reciprocal rank of the first relevant id in the ranked list (1-indexed):
// 1/rank, or 0 when no relevant id is found or the relevant set is empty.
func mrr(ranked []string, relevant map[string]struct{}) float64 {
	if len(relevant) == 0 {
		return 0
	}
	for i, id := range ranked {
		if _, ok := relevant[id]; ok {
			return 1.0 / float64(i+1)
		}
	}
	return 0
}
