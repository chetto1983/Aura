package documents

import (
	"math"
	"testing"
)

// The local llama.cpp sidecar ignores the OpenAI `dimensions` request parameter and
// always answers at the model's native width, so narrowing to the width the vector
// index was built for happens client-side. These cases pin that contract.
func TestTruncateMRL(t *testing.T) {
	t.Parallel()

	unit := func(t *testing.T, vec []float64) {
		t.Helper()
		var sum float64
		for _, v := range vec {
			sum += v * v
		}
		if math.Abs(math.Sqrt(sum)-1) > 1e-9 {
			t.Fatalf("truncated vector has norm %v, want unit length", math.Sqrt(sum))
		}
	}

	t.Run("already at width is returned untouched", func(t *testing.T) {
		t.Parallel()
		in := []float64{3, 4}
		got, err := truncateMRL(in, 2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Deliberately NOT renormalised: a server that already answers at the
		// requested width is trusted, so its scale is preserved byte for byte.
		if got[0] != 3 || got[1] != 4 {
			t.Fatalf("got %v, want the input unchanged", got)
		}
	})

	t.Run("wider vector keeps the leading components and is renormalised", func(t *testing.T) {
		t.Parallel()
		got, err := truncateMRL([]float64{3, 4, 99, 99}, 2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("got width %d, want 2", len(got))
		}
		// 3,4 has norm 5 -> 0.6,0.8. The dropped tail must not influence the result.
		if math.Abs(got[0]-0.6) > 1e-9 || math.Abs(got[1]-0.8) > 1e-9 {
			t.Fatalf("got %v, want [0.6 0.8]", got)
		}
		unit(t, got)
	})

	t.Run("native width narrows to the index width at unit length", func(t *testing.T) {
		t.Parallel()
		in := make([]float64, 1024)
		for i := range in {
			in[i] = float64(i%7) + 1
		}
		got, err := truncateMRL(in, 768)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 768 {
			t.Fatalf("got width %d, want 768", len(got))
		}
		unit(t, got)
	})

	t.Run("narrower than the index is refused rather than padded", func(t *testing.T) {
		t.Parallel()
		// Padding would let a mismatched model write silently corrupt vectors.
		if _, err := truncateMRL(make([]float64, 512), 768); err == nil {
			t.Fatal("want an error for a vector narrower than the configured width")
		}
	})

	t.Run("an all-zero leading slice is refused", func(t *testing.T) {
		t.Parallel()
		// Renormalising would divide by zero; a zero vector is never a valid embedding.
		in := make([]float64, 8)
		in[7] = 1
		if _, err := truncateMRL(in, 4); err == nil {
			t.Fatal("want an error when every retained component is zero")
		}
	})
}
