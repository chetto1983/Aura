package tools

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/semindex"
	"github.com/chetto1983/aura/internal/toolselectstore"
)

// vecEmbedder is a deterministic fake Embedder returning a fixed vector for any text
// (the learnedBoost needs an embedder only to construct; rerank is fed vectors
// directly). Distinct from bagEmbedder so these tests control the geometry exactly.
type vecEmbedder struct{ v []float64 }

func (e vecEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for i := range texts {
		out[i] = append([]float64(nil), e.v...)
	}
	return out, nil
}

// onehot returns a unit vector on the given 3-axis dimension (orthogonal concepts).
func onehot(dim int) []float64 {
	v := make([]float64, 3)
	v[dim] = 1
	return v
}

func ex(tool string, dim int) toolselectstore.LabeledVec {
	return toolselectstore.LabeledVec{Tool: tool, Vec: onehot(dim), Query: tool}
}

// Req-5b: folding confirmed (query->tool) exemplars + refreshing the per-tool
// centroid raises the held-out tool's rank vs the no-fold baseline.
func TestLearnedBoost_FoldRaisesHeldOutRank(t *testing.T) {
	b := newLearnedBoost(vecEmbedder{v: onehot(0)})
	if b == nil {
		t.Fatal("newLearnedBoost returned nil with an embedder")
	}

	// Stage-1: a near-tie where the WRONG tool (other) edges out web_search by a hair.
	// web_search is the correct tool for axis-0 queries.
	stage1 := []semindex.Scored{
		{Label: "other", Score: 0.61},
		{Label: "web_search", Score: 0.60},
	}
	qVec := onehot(0) // the held-out query lives on web_search's concept axis

	// Baseline: no fold => strict no-op, the wrong tool stays #1.
	base := b.rerank(stage1, qVec)
	if base[0].Label != "other" {
		t.Fatalf("baseline expected the un-boosted order (other #1); got %s", base[0].Label)
	}

	// Fold >= minSupport confirmed web_search exemplars on axis-0 (the query's axis).
	b.Fold([]toolselectstore.LabeledVec{
		ex("web_search", 0), ex("web_search", 0), ex("web_search", 0),
	})
	boosted := b.rerank(stage1, qVec)
	if boosted[0].Label != "web_search" {
		t.Errorf("after folding confirmed exemplars + refresh, web_search should rank #1; got %s (%+v)", boosted[0].Label, boosted)
	}
}

// Req-5c (guard #1, min-support floor): a poisoned label below N<minSupport cannot
// flip a seed-correct routing — the boost is a STRICT no-op and the order is identical.
func TestLearnedBoost_BelowMinSupportIsStrictNoOp(t *testing.T) {
	b := newLearnedBoost(vecEmbedder{v: onehot(0)})
	stage1 := []semindex.Scored{
		{Label: "web_search", Score: 0.80}, // the seed-correct routing
		{Label: "poisoned", Score: 0.70},
	}
	qVec := onehot(0)

	// Inject 2 poisoned exemplars (< minSupport=3) for "poisoned" right on the query's
	// axis — maximally tempting, but below the floor so it must be ignored.
	b.Fold([]toolselectstore.LabeledVec{ex("poisoned", 0), ex("poisoned", 0)})
	got := b.rerank(stage1, qVec)
	if !sameOrder(got, stage1) {
		t.Errorf("a sub-min-support poisoned label changed the order: %+v (want byte-identical to seed-only)", got)
	}
	if got[0].Label != "web_search" {
		t.Errorf("poisoned label flipped the seed-correct routing below min-support; got #1 = %s", got[0].Label)
	}
}

// Req-5c (guard #2, boost cap): even at >= minSupport, the epsilon clamp keeps a
// poisoned centroid from outranking a strong base description match.
func TestLearnedBoost_BoostCapProtectsStrongBaseMatch(t *testing.T) {
	b := newLearnedBoost(vecEmbedder{v: onehot(0)})
	// web_search has a STRONG base score; poisoned is far behind. The boost on poisoned
	// is clamped to epsilon*desc_cos, which cannot close the gap.
	stage1 := []semindex.Scored{
		{Label: "web_search", Score: 0.90},
		{Label: "poisoned", Score: 0.50},
	}
	qVec := onehot(0)
	b.Fold([]toolselectstore.LabeledVec{ex("poisoned", 0), ex("poisoned", 0), ex("poisoned", 0)})
	got := b.rerank(stage1, qVec)
	if got[0].Label != "web_search" {
		t.Errorf("boost cap failed: a poisoned label (>=min-support) outranked a strong base match; #1 = %s (%+v)", got[0].Label, got)
	}
}

// The learned boost is keyed by tool: a tool with enough support but whose centroid is
// ORTHOGONAL to the query (cosine below tau) is a strict no-op for that query.
func TestLearnedBoost_BelowTauIsNoOp(t *testing.T) {
	b := newLearnedBoost(vecEmbedder{v: onehot(0)})
	stage1 := []semindex.Scored{
		{Label: "web_search", Score: 0.61},
		{Label: "document_search", Score: 0.60},
	}
	qVec := onehot(0) // query on axis-0
	// document_search's exemplars live on axis-1 (orthogonal) => centroid_cos=0 < tau.
	b.Fold([]toolselectstore.LabeledVec{ex("document_search", 1), ex("document_search", 1), ex("document_search", 1)})
	got := b.rerank(stage1, qVec)
	if !sameOrder(got, stage1) {
		t.Errorf("an orthogonal-centroid tool (below tau) changed the order: %+v", got)
	}
}

// A nil boost (no embedder wired) leaves the stage-1 order untouched (the no-fold path
// preserves Plan-03's ranking).
func TestLearnedBoost_NilIsPassthrough(t *testing.T) {
	var b *learnedBoost
	stage1 := []semindex.Scored{{Label: "a", Score: 0.5}, {Label: "b", Score: 0.4}}
	if got := b.rerank(stage1, onehot(0)); !sameOrder(got, stage1) {
		t.Errorf("nil boost mutated the order: %+v", got)
	}
}

func sameOrder(a, b []semindex.Scored) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Label != b[i].Label {
			return false
		}
	}
	return true
}
