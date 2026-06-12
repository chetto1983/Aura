package semindex

import (
	"context"
	"sort"
	"sync"
)

// Ranker is the PerItem-mode wrapper (D-01): top-K cosine over individual
// document vectors. It holds the Embedder seam and one sync.RWMutex; Add/AddVecs
// take the write lock and APPEND to the bank (the D-03 incremental door), while
// Rank/RankVecs take the read lock. The top-K selection mirrors bm25 rank
// (bm25.go:130-145): stable sort desc with a deterministic insertion-index
// tie-break — load-bearing for top-K stability and the byte-stable manifest.
// No Margin here — that lives on the Classifier only.
type Ranker struct {
	embed Embedder

	mu   sync.RWMutex
	bank []Item // append-only per-item bank; insertion order is the tie-break key
}

// RankResult is the text-in Rank result: the top-K Results plus any embedder
// Err (Req-6 error surface — never a silent empty ranking on infra failure).
type RankResult struct {
	Results []Scored
	Err     error
}

// NewRanker returns a Ranker over the given embedder, or nil if embed is nil.
func NewRanker(embed Embedder) *Ranker {
	if embed == nil {
		return nil
	}
	return &Ranker{embed: embed}
}

// AddVecs appends caller-provided items to the bank under the write lock,
// L2-normalizing each vector (the D-03 incremental-append door).
func (r *Ranker) AddVecs(items ...Item) {
	if r == nil || len(items) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, it := range items {
		r.bank = append(r.bank, Item{Label: it.Label, Vec: l2normalize(it.Vec)})
	}
}

// Add embeds texts and appends them as items labeled by their own text (D-01
// text-in door). On an embedder error nothing is appended and the error is
// returned verbatim (Req-6). The label is the source text so the caller can map
// a result back to its query/tool string.
func (r *Ranker) Add(ctx context.Context, texts ...string) error {
	if r == nil || len(texts) == 0 {
		return nil
	}
	vecs, err := r.embed.Embed(ctx, texts)
	if err != nil {
		return err
	}
	items := make([]Item, 0, len(texts))
	for i, t := range texts {
		if i < len(vecs) {
			items = append(items, Item{Label: t, Vec: vecs[i]})
		}
	}
	r.AddVecs(items...)
	return nil
}

// Len reports the current bank size (the incremental-append assertion lever).
func (r *Ranker) Len() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.bank)
}

// RankVecs returns the top-K items by cosine to the query vector, descending,
// ties broken by ascending insertion index (deterministic). k<=0 or k>bank
// returns all matching items. An empty bank returns an empty slice.
func (r *Ranker) RankVecs(vec []float64, k int) []Scored {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.bank) == 0 {
		return []Scored{}
	}
	q := l2normalize(vec)
	type indexed struct {
		Scored
		idx int
	}
	scored := make([]indexed, 0, len(r.bank))
	for i, it := range r.bank {
		scored = append(scored, indexed{Scored{Label: it.Label, Score: cosine(q, it.Vec)}, i})
	}
	sort.SliceStable(scored, func(a, b int) bool {
		if scored[a].Score != scored[b].Score {
			return scored[a].Score > scored[b].Score
		}
		return scored[a].idx < scored[b].idx
	})
	if k > 0 && k < len(scored) {
		scored = scored[:k]
	}
	out := make([]Scored, len(scored))
	for i := range scored {
		out[i] = scored[i].Scored
	}
	return out
}

// Rank embeds a single query text and returns its top-K (D-01 text-in door).
// The embedder error is carried on RankResult.Err (Req-6), never swallowed.
func (r *Ranker) Rank(ctx context.Context, query string, k int) RankResult {
	if r == nil {
		return RankResult{}
	}
	vecs, err := r.embed.Embed(ctx, []string{query})
	if err != nil {
		return RankResult{Err: err}
	}
	if len(vecs) != 1 || len(vecs[0]) == 0 {
		return RankResult{Results: []Scored{}}
	}
	return RankResult{Results: r.RankVecs(vecs[0], k)}
}
