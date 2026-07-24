package semindex

import "math"

// This file is the lock-free pure-math core (D-01): no sync, no context, no I/O.
// cosine/l2normalize/centroid are lifted verbatim from
// reasoning_classifier.go:218-264 (cosineSimilarity→cosine, meanNormalize→
// centroid); margin lifts the top-2 argmax-with-second loop (lines 155-169).
// Brute-force only — no ANN/HNSW import here or anywhere in the package (Req-8).

func l2normalize(v []float64) []float64 {
	var n float64
	for _, x := range v {
		n += x * x
	}
	n = math.Sqrt(n)
	if n == 0 {
		return v
	}
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = x / n
	}
	return out
}

// Normalize exposes L2-normalization for consumers that retain the normalized
// query vector without duplicating the core math. RankVecs normalizes internally.
func Normalize(v []float64) []float64 { return l2normalize(v) }

// cosine assumes both inputs are already L2-normalized (anchors and the query
// both are), so it is a plain dot product. On a length mismatch it returns the
// -2.0 "impossible cosine" sentinel — load-bearing for the no-best fallback
// path and a non-panicking guard against malformed input (T-08.2-01).
func cosine(a, b []float64) float64 {
	if len(a) != len(b) {
		return -2.0
	}
	var d float64
	for i := range a {
		d += a[i] * b[i]
	}
	return d
}

// centroid averages the vectors then L2-normalizes the mean. An empty bank
// returns nil deterministically (T-08.2-03: no garbage centroid).
func centroid(vecs [][]float64) []float64 {
	if len(vecs) == 0 {
		return nil
	}
	acc := make([]float64, len(vecs[0]))
	for _, v := range vecs {
		for i := range acc {
			if i < len(v) {
				acc[i] += v[i]
			}
		}
	}
	for i := range acc {
		acc[i] /= float64(len(vecs))
	}
	return l2normalize(acc)
}

// margin returns the top-2 gap (best - second) over a score slice using the
// same argmax-with-second sweep the reasoning classifier uses, seeded with the
// -2.0 sentinel so a single score yields best-(-2.0). An empty slice is 0.
func margin(scores []float64) float64 {
	if len(scores) == 0 {
		return 0
	}
	best, second := -2.0, -2.0
	for _, s := range scores {
		if s > best {
			second, best = best, s
		} else if s > second {
			second = s
		}
	}
	return best - second
}
