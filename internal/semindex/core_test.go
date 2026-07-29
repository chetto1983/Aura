package semindex

import (
	"math"
	"testing"
)

const floatTol = 1e-9

func approxEq(a, b float64) bool { return math.Abs(a-b) <= floatTol }

func TestCosine(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		a, b []float64
		want float64
	}{
		{"identical unit x", []float64{1, 0, 0}, []float64{1, 0, 0}, 1},
		{"orthogonal", []float64{1, 0, 0}, []float64{0, 1, 0}, 0},
		{"opposite", []float64{1, 0, 0}, []float64{-1, 0, 0}, -1},
		{"forty-five deg", []float64{1, 0}, []float64{1 / math.Sqrt2, 1 / math.Sqrt2}, 1 / math.Sqrt2},
		{"length mismatch sentinel", []float64{1, 0, 0}, []float64{1, 0}, -2.0},
		{"empty equal length", []float64{}, []float64{}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := cosine(tc.a, tc.b); !approxEq(got, tc.want) {
				t.Fatalf("cosine(%v,%v) = %v; want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func TestCosineLengthMismatchNeverPanics(t *testing.T) {
	t.Parallel()
	// T-08.2-01: a length-mismatched cosine must return the -2.0 sentinel,
	// never index out of range / panic.
	if got := cosine([]float64{1, 2, 3, 4}, []float64{1}); got != -2.0 {
		t.Fatalf("len-mismatch cosine = %v; want -2.0 sentinel", got)
	}
}

func TestL2Normalize(t *testing.T) {
	t.Parallel()
	t.Run("scales to unit norm", func(t *testing.T) {
		t.Parallel()
		out := l2normalize([]float64{3, 4})
		if !approxEq(out[0], 0.6) || !approxEq(out[1], 0.8) {
			t.Fatalf("l2normalize([3,4]) = %v; want [0.6 0.8]", out)
		}
		var n float64
		for _, x := range out {
			n += x * x
		}
		if !approxEq(math.Sqrt(n), 1) {
			t.Fatalf("normalized norm = %v; want 1", math.Sqrt(n))
		}
	})
	t.Run("zero vector returned unchanged", func(t *testing.T) {
		t.Parallel()
		in := []float64{0, 0, 0}
		out := l2normalize(in)
		for i := range out {
			if out[i] != 0 {
				t.Fatalf("zero vector mutated: %v", out)
			}
		}
	})
}

func TestCentroid(t *testing.T) {
	t.Parallel()
	t.Run("nil bank yields nil", func(t *testing.T) {
		t.Parallel()
		// T-08.2-03: empty/nil bank must return nil deterministically (no garbage).
		if got := centroid(nil); got != nil {
			t.Fatalf("centroid(nil) = %v; want nil", got)
		}
		if got := centroid([][]float64{}); got != nil {
			t.Fatalf("centroid(empty) = %v; want nil", got)
		}
	})
	t.Run("mean of one-hot axes is normalized", func(t *testing.T) {
		t.Parallel()
		// Mean of [1,0,0] and [0,1,0] is [0.5,0.5,0]; L2-normalized → [√½,√½,0].
		got := centroid([][]float64{{1, 0, 0}, {0, 1, 0}})
		want := []float64{1 / math.Sqrt2, 1 / math.Sqrt2, 0}
		for i := range want {
			if !approxEq(got[i], want[i]) {
				t.Fatalf("centroid = %v; want %v", got, want)
			}
		}
	})
	t.Run("single vector normalized", func(t *testing.T) {
		t.Parallel()
		got := centroid([][]float64{{2, 0}})
		if !approxEq(got[0], 1) || !approxEq(got[1], 0) {
			t.Fatalf("centroid([[2,0]]) = %v; want [1 0]", got)
		}
	})
}

func TestMargin(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		scores []float64
		want   float64
	}{
		{"two clear scores", []float64{0.9, 0.3}, 0.6},
		{"unsorted picks top two", []float64{0.3, 0.9, 0.5}, 0.4},
		{"single score has no second", []float64{0.7}, 0.7 - (-2.0)},
		{"empty is zero", []float64{}, 0},
		{"tied top two zero margin", []float64{0.5, 0.5}, 0},
		{"negative scores", []float64{-0.2, -0.5}, 0.3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := margin(tc.scores); !approxEq(got, tc.want) {
				t.Fatalf("margin(%v) = %v; want %v", tc.scores, got, tc.want)
			}
		})
	}
}

// TestGoldenMarginOverSyntheticVectors is the D-01 test lever: golden margins
// computed end-to-end over hand-built orthogonal one-hot vectors (no embedder).
func TestGoldenMarginOverSyntheticVectors(t *testing.T) {
	t.Parallel()
	// Three orthogonal one-hot centroids; a query tilted toward the x-axis.
	centroids := [][]float64{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}
	q := l2normalize([]float64{0.8, 0.6, 0})
	scores := make([]float64, len(centroids))
	for i, c := range centroids {
		scores[i] = cosine(q, c)
	}
	// q·[1,0,0] = 0.8, q·[0,1,0] = 0.6, q·[0,0,1] = 0 → margin = 0.8 - 0.6 = 0.2.
	if got := margin(scores); !approxEq(got, 0.2) {
		t.Fatalf("golden margin = %v; want 0.2 (scores %v)", got, scores)
	}
}
