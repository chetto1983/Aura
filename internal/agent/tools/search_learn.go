package tools

import (
	"context"
	"sort"
	"sync"

	"github.com/chetto1983/aura/internal/semindex"
	"github.com/chetto1983/aura/internal/toolselectstore"
)

// The learned-signal stage-2 boost (D-07). Free-text tool_search is a two-stage
// ranker: stage-1 is the Plan-03 description cosine (search.go/search_fusion.go);
// stage-2 is a per-tool centroid of oracle-CONFIRMED query exemplars, folded by the
// active-learning loop (internal/toolselectlearn). The stage-2 signal is positives-
// only Rocchio (gamma=0): each tool's centroid is the mean of the query embeddings
// the oracle confirmed should route to it — a prototypical-network prototype, NOT a
// kNN point (spike-057: "centroid, not kNN" — lower variance, noise-robust, O(1)/tool).
//
// Fusion is a margin-gated CONDITIONAL boost, never always-on interpolation: the
// centroid re-ranks ONLY when it is confident (centroid_cos > tau) AND the tool has
// enough support; below threshold it is a STRICT no-op so the base description
// ranking + curated seeds stand untouched. When it fires the boost is a small
// additive term with alpha (base description) >= beta (learned) and boost <= eps *
// desc_cos (the PRPO epsilon clamp) — "never rank worse than the safe model".

const (
	// minSupport is the anti-amplification floor (Req-5c guard #1, the highest-value
	// one): a tool's centroid is eligible only after this many CONFIRMED exemplars.
	// Below it a single example is a noisy 1-NN point, not a prototype, so it stays a
	// strict no-op and a poisoned 1-2-example label cannot move the ranking. N>=3-5
	// (D-07); 3 is the floor.
	minSupport = 3
	// learnedTau is the confidence gate: the centroid boost fires only when the query's
	// cosine to a tool's learned centroid clears this. Below it the learned signal is
	// not confident and the stage-1 description ranking stands unchanged.
	learnedTau = 0.35
	// learnedBeta scales the learned centroid's contribution (beta in alpha>=beta). The
	// description cosine carries weight alpha=1 (it IS the stage-1 score); the learned
	// term is added with this smaller weight so the base ranking stays primary.
	learnedBeta = 0.5
	// learnedEpsilon is the PRPO clamp (Req-5c guard #2, the boost cap): the additive
	// boost can never exceed eps * desc_cos, so a confident-but-wrong centroid still
	// cannot outrank a strong base description match. One-line clamp.
	learnedEpsilon = 0.25
)

// --- ToolSearch wiring for the tool-selection active-learning loop (D-06/D-07) ---

// EnableLearnedBoost attaches the stage-2 per-tool centroid boost over the given
// embedder (called once by the composition root). nil-safe; a nil embedder leaves the
// learned signal disabled (the stage-1 ranking stands alone).
func (ts *ToolSearch) EnableLearnedBoost(embed semindex.Embedder) {
	b := newLearnedBoost(embed)
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.learned = b
}

// FoldLearned replaces the per-tool centroid bank from the full confirmed-example set
// (the learner's Refresh hook re-folds the whole :ToolSelectionExample set after each
// save). nil-safe.
func (ts *ToolSearch) FoldLearned(examples []toolselectstore.LabeledVec) {
	ts.mu.Lock()
	b := ts.learned
	ts.mu.Unlock()
	b.Fold(examples)
}

// RankForLearner is the toolselectlearn.Ranker seam: it embeds the request and ranks
// it against the STAGE-1 description bank (NOT the learned centroids — the loop must
// not self-confirm; guard #4 oracle-in-loop), returning the top-1 tool name and the
// top-2 cosine margin (the free-vs-escalate confidence signal). ok is false when the
// bank is empty or the embedder is down (then the detector uses only the embedding-free
// shell/fs heuristic and the oracle escalates). It builds the bank on demand so a
// post-turn Observe sees the same deferred corpus a query would.
func (ts *ToolSearch) RankForLearner(query string) (string, float64, bool) {
	ranker, _, _, err := ts.ensureBank(context.Background(), query)
	if err != nil || ranker == nil {
		return "", 0, false
	}
	qVec, err := ts.embedQuery(context.Background(), query)
	if err != nil || len(qVec) == 0 {
		return "", 0, false
	}
	scored := ranker.RankVecs(qVec, 2)
	if len(scored) == 0 {
		return "", 0, false
	}
	margin := scored[0].Score
	if len(scored) > 1 {
		margin = scored[0].Score - scored[1].Score
	}
	return scored[0].Label, margin, true
}

// learnedBoost holds the per-tool learned-centroid bank (keyed by tool name) plus the
// per-tool support count (the min-support gate). It re-ranks a stage-1 result list as
// a margin-gated conditional boost. Concurrency: one RWMutex guards the bank + counts;
// Fold takes the write lock, rerank takes the read lock — so a live Refresh
// (re-folding from Neo4j after a save) races neither a query nor itself.
type learnedBoost struct {
	embed semindex.Embedder

	mu      sync.RWMutex
	bank    *semindex.Classifier // per-tool centroids (Centroid mode keyed by tool)
	support map[string]int       // confirmed-exemplar count per tool (the min-support gate)
}

// newLearnedBoost builds an empty boost over the given embedder. A nil embedder
// yields a nil boost (no learned signal — the stage-1 ranking stands alone).
func newLearnedBoost(embed semindex.Embedder) *learnedBoost {
	if embed == nil {
		return nil
	}
	return &learnedBoost{embed: embed, support: make(map[string]int)}
}

// Fold rebuilds the per-tool centroid bank from the full confirmed-example set
// (called by the learner's Refresh after a save). It REPLACES the bank wholesale so a
// query-keyed UPSERT in the store (the latest label per query) is reflected exactly —
// stale labels never linger. Each example's query vector folds into its tool's
// centroid via AddVecs; the support count is the number of distinct exemplars per tool.
func (b *learnedBoost) Fold(examples []toolselectstore.LabeledVec) {
	if b == nil {
		return
	}
	fresh := semindex.NewClassifier(b.embed)
	counts := make(map[string]int, len(examples))
	for _, ex := range examples {
		if ex.Tool == "" || len(ex.Vec) == 0 {
			continue
		}
		fresh.AddVecs(ex.Tool, ex.Vec)
		counts[ex.Tool]++
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.bank = fresh
	b.support = counts
}

// rerank applies the margin-gated stage-2 boost to a stage-1 result list. qVec is the
// query embedding (reused from stage-1 — no second embed). For each stage-1 candidate
// whose tool has >= minSupport exemplars AND whose centroid cosine clears learnedTau,
// it adds a clamped boost (beta * centroid_cos, capped at epsilon * desc_cos) to the
// stage-1 score, then stable-re-sorts. Tools below support/tau are untouched (strict
// no-op). An empty bank, a nil boost, or a zero query vector returns the input order
// byte-for-byte (the no-fold path leaves Plan-03's ranking unchanged).
func (b *learnedBoost) rerank(stage1 []semindex.Scored, qVec []float64) []semindex.Scored {
	if b == nil || len(stage1) == 0 || len(qVec) == 0 {
		return stage1
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.bank == nil || len(b.support) == 0 {
		return stage1
	}
	type fused struct {
		semindex.Scored
		idx     int
		boosted float64
	}
	out := make([]fused, len(stage1))
	changed := false
	for i, s := range stage1 {
		boosted := s.Score
		if b.support[s.Label] >= minSupport {
			if cc, ok := b.bank.GroupCosine(s.Label, qVec); ok && cc > learnedTau {
				boost := learnedBeta * cc
				if capBoost := learnedEpsilon * s.Score; boost > capBoost {
					boost = capBoost // PRPO epsilon clamp (guard #2): boost <= eps * desc_cos
				}
				if boost > 0 {
					boosted = s.Score + boost
					changed = true
				}
			}
		}
		out[i] = fused{Scored: s, idx: i, boosted: boosted}
	}
	if !changed {
		return stage1 // strict no-op: nothing cleared support+tau
	}
	sort.SliceStable(out, func(a, c int) bool {
		if out[a].boosted != out[c].boosted {
			return out[a].boosted > out[c].boosted
		}
		return out[a].idx < out[c].idx // deterministic tie-break = stage-1 order
	})
	res := make([]semindex.Scored, len(out))
	for i := range out {
		res[i] = out[i].Scored
	}
	return res
}
