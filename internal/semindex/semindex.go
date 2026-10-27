// Package semindex is Aura's reusable embedding-index core: one shared
// lock-free pure-math layer (cosine/centroid/margin/bank ops over [][]float64)
// plus the Classifier (Centroid mode: argmax over per-group means → best label +
// top-2 Margin), which holds the Embedder seam and one sync.RWMutex; the math core
// stays pure (no ctx, no I/O, no lock). Brute-force cosine only — no ANN/HNSW
// (Req-8): the immutable, small banks make a naive O(N) scan sub-millisecond.
package semindex

import "context"

// Embedder is the narrow embedding seam the index needs. It matches
// embeddings.Client.Embed exactly so the production embedding sidecar
// client satisfies it without an adapter. Owned here (the shared core) so the
// reasoning classifier depends on it; prompt.Embedder is a type alias of this
// (D-01, no-adapter preserved).
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float64, error)
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
