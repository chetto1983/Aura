package agenttest

import (
	"context"
	"sync/atomic"
)

// UniformEmbedder embeds every text as the same vector and counts its calls. Wired into the
// reasoning classifier, it makes every seed tier score alike; semindex's argmax gives a tie
// to the first label in sorted order, so the verdict is high at margin 0, below the teacher
// margin: a seeds decision that asks the teacher.
type UniformEmbedder struct{ calls atomic.Int64 }

// Embed returns the same unit vector for every text.
func (e *UniformEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	e.calls.Add(1)
	vecs := make([][]float64, len(texts))
	for i := range vecs {
		vecs[i] = []float64{1, 0, 0}
	}
	return vecs, nil
}

// Calls is the number of Embed calls so far.
func (e *UniformEmbedder) Calls() int64 { return e.calls.Load() }
