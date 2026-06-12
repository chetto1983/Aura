package semindex

import (
	"context"
	"sort"
	"sync"
)

// Classifier is the Centroid-mode wrapper (D-01): it folds each group's
// exemplars into a per-group mean (centroid) and answers a query with the
// argmax label plus the top-2 Margin. It holds the Embedder seam and one
// sync.RWMutex; the math stays in the lock-free core. Mirrors the reasoning
// classifier's anchor bank (reasoning_classifier.go:95-209) — same Centroid
// mode, made label-generic. Margin lives on this result ONLY, never on Ranker.
type Classifier struct {
	embed Embedder

	mu        sync.RWMutex
	groups    map[string][][]float64 // raw L2-normalized exemplars per group
	centroids map[string][]float64   // memoized per-group centroid; nil ⇒ rebuild
}

// NewClassifier returns a Classifier over the given embedder, or nil if embed is
// nil (callers treat nil as "no classifier wired").
func NewClassifier(embed Embedder) *Classifier {
	if embed == nil {
		return nil
	}
	return &Classifier{
		embed:     embed,
		groups:    make(map[string][][]float64),
		centroids: make(map[string][]float64),
	}
}

// AddVecs folds caller-provided vectors into the group's exemplar bank under the
// write lock. The group centroid is invalidated so the next Rank recomputes it.
func (c *Classifier) AddVecs(label string, vecs ...[]float64) {
	if c == nil || len(vecs) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, v := range vecs {
		c.groups[label] = append(c.groups[label], l2normalize(v))
	}
	delete(c.centroids, label) // invalidate — rebuilt lazily on next Rank
}

// Add embeds texts and folds them into the group (D-01 text-in door). A build
// failure is NOT persisted: nothing is added on error, so a retry self-heals
// (mirror of ensureAnchors). Returns the embedder error verbatim.
func (c *Classifier) Add(ctx context.Context, label string, texts ...string) error {
	if c == nil || len(texts) == 0 {
		return nil
	}
	vecs, err := c.embed.Embed(ctx, texts)
	if err != nil {
		return err
	}
	c.AddVecs(label, vecs...)
	return nil
}

// RankVecs scores a query vector against every group centroid and returns the
// argmax Verdict (label + score + top-2 Margin). An empty bank yields Ok=false.
func (c *Classifier) RankVecs(vec []float64) Verdict {
	if c == nil {
		return Verdict{}
	}
	c.mu.Lock() // write lock: may memoize centroids
	defer c.mu.Unlock()
	if len(c.groups) == 0 {
		return Verdict{}
	}
	q := l2normalize(vec)
	labels := make([]string, 0, len(c.groups))
	for label := range c.groups {
		labels = append(labels, label)
	}
	sort.Strings(labels) // deterministic argmax tie-break
	best, bestScore := "", -2.0
	scores := make([]float64, 0, len(labels))
	for _, label := range labels {
		cen := c.centroids[label]
		if cen == nil {
			cen = centroid(c.groups[label])
			c.centroids[label] = cen
		}
		s := cosine(q, cen)
		scores = append(scores, s)
		if s > bestScore {
			best, bestScore = label, s
		}
	}
	if best == "" {
		return Verdict{}
	}
	return Verdict{Label: best, Score: bestScore, Margin: margin(scores), Ok: true}
}

// Rank embeds a single query text and ranks it (D-01 text-in door). The
// embedder error is surfaced verbatim (Req-6 error surface).
func (c *Classifier) Rank(ctx context.Context, text string) (Verdict, error) {
	if c == nil {
		return Verdict{}, nil
	}
	vecs, err := c.embed.Embed(ctx, []string{text})
	if err != nil {
		return Verdict{}, err
	}
	if len(vecs) != 1 || len(vecs[0]) == 0 {
		return Verdict{}, nil
	}
	return c.RankVecs(vecs[0]), nil
}
