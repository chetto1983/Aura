// Package semindex is Aura's reusable embedding-index core: one shared
// lock-free pure-math layer (cosine/centroid/margin/bank ops over [][]float64)
// plus two typed wrappers — Classifier (Centroid mode: argmax over per-group
// means → best label + top-2 Margin) and Ranker (PerItem mode: top-K cosine
// over individual docs → []Scored). Both wrappers hold the Embedder seam and one
// sync.RWMutex; the math core stays pure (no ctx, no I/O, no lock). Brute-force
// cosine only — no ANN/HNSW (Req-8): the immutable, small banks make a naive
// O(N) scan sub-millisecond.
package semindex

import "context"

// Embedder is the narrow embedding seam the index needs. It matches
// documents.EmbeddingClient.Embed exactly so the production granite sidecar
// client satisfies it without an adapter. Owned here (the shared core) so both
// the tool ranker and the reasoning classifier depend on one interface;
// prompt.Embedder becomes a type alias of this (D-01, no-adapter preserved).
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float64, error)
}

// Item is one PerItem bank entry: a label paired with its (caller-provided or
// embedded) vector. The Ranker appends these incrementally (D-03).
type Item struct {
	Label string
	Vec   []float64
}

// Scored pairs a label with its cosine score against a query. It is the Ranker's
// PerItem result shape (mirrors the bm25 scoredDoc analog, exported with a
// label). Margin lives on the Classifier result only — it does not pollute this.
type Scored struct {
	Label string
	Score float64
}

// Verdict is the Classifier's Centroid result: the argmax label, its score, and
// the top-2 Margin (best - second). Ok is false when the bank is empty or the
// query vector cannot be scored, so callers can fall back.
type Verdict struct {
	Label  string
	Score  float64
	Margin float64
	Ok     bool
}
