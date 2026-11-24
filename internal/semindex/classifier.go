package semindex

import (
	"slices"
	"sort"
	"sync"
)

// Classifier is a labelled exemplar bank (D-01) answering a query with the argmax
// label plus the top-2 Margin. A group is scored either by its centroid (RankVecs) or
// by its exemplars nearest to the query (RankNearest). Callers embed; the bank takes
// vectors. It holds one sync.RWMutex; the math stays in the lock-free core.
type Classifier struct {
	mu        sync.RWMutex
	groups    map[string][][]float64 // raw L2-normalized exemplars per group
	centroids map[string][]float64   // memoized per-group centroid; nil ⇒ rebuild
}

// NewClassifier returns an empty Classifier. A nil *Classifier is still a valid
// "no classifier wired": every method is nil-receiver safe.
func NewClassifier() *Classifier {
	return &Classifier{
		groups:    make(map[string][][]float64),
		centroids: make(map[string][]float64),
	}
}

// AddVecs folds caller-provided vectors into the group's exemplar bank under the
// write lock. The group centroid is invalidated so the next RankVecs recomputes it.
func (c *Classifier) AddVecs(label string, vecs ...[]float64) {
	if c == nil || len(vecs) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, v := range vecs {
		c.groups[label] = append(c.groups[label], l2normalize(v))
	}
	delete(c.centroids, label) // invalidate — rebuilt lazily on next RankVecs
}

// RankVecs scores a query vector against every group centroid and returns the
// argmax Verdict (label + score + top-2 Margin). An empty bank yields Ok=false.
func (c *Classifier) RankVecs(vec []float64) Verdict {
	if c == nil {
		return Verdict{}
	}
	c.mu.Lock() // write lock: may memoize centroids
	defer c.mu.Unlock()
	q := l2normalize(vec)
	return c.argmax(func(label string) float64 {
		cen := c.centroids[label]
		if cen == nil {
			cen = centroid(c.groups[label])
			c.centroids[label] = cen
		}
		return cosine(q, cen)
	})
}

// RankNearest scores each group by the mean cosine of its k exemplars nearest to the
// query and returns the argmax Verdict. A group that spans several intents has its
// centroid between them and far from each, so a query matching one of those intents
// loses to any compact group nearby; its nearest exemplars do not move. A group with
// fewer than k exemplars is scored over all of them.
func (c *Classifier) RankNearest(vec []float64, k int) Verdict {
	if c == nil || k < 1 {
		return Verdict{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	q := l2normalize(vec)
	return c.argmax(func(label string) float64 { return meanNearest(q, c.groups[label], k) })
}

// argmax applies score to every group in label order, so a tie goes to the first label.
// The caller holds c.mu.
func (c *Classifier) argmax(score func(label string) float64) Verdict {
	if len(c.groups) == 0 {
		return Verdict{}
	}
	labels := make([]string, 0, len(c.groups))
	for label := range c.groups {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	best, bestScore := "", -2.0
	scores := make([]float64, 0, len(labels))
	for _, label := range labels {
		s := score(label)
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

func meanNearest(q []float64, exemplars [][]float64, k int) float64 {
	sims := make([]float64, len(exemplars))
	for i, e := range exemplars {
		sims[i] = cosine(q, e)
	}
	slices.Sort(sims)
	nearest := sims[max(0, len(sims)-k):]
	var sum float64
	for _, s := range nearest {
		sum += s
	}
	return sum / float64(len(nearest))
}
