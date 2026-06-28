package eval

import (
	"math"
	"testing"
)

const metricEps = 1e-9

func approxEqual(a, b float64) bool { return math.Abs(a-b) < metricEps }

func TestNDCGAtK(t *testing.T) {
	// IDCG for two relevant ids = 1/log2(2) + 1/log2(3) = 1 + 0.6309297535714574.
	idcg2 := 1.0 + 1.0/math.Log2(3)
	cases := []struct {
		name   string
		ranked []string
		rel    []string
		k      int
		want   float64
	}{
		{"perfect front-loads relevant", []string{"a", "c", "b", "d"}, []string{"a", "c"}, 4, 1.0},
		{"relevant at ranks 1 and 3", []string{"a", "b", "c", "d"}, []string{"a", "c"}, 4, 1.5 / idcg2},
		{"top relevant within k", []string{"c", "a", "b"}, []string{"c"}, 2, 1.0},
		{"relevant outside k scores zero", []string{"a", "b", "c"}, []string{"c"}, 2, 0.0},
		{"empty relevant set", []string{"a", "b"}, nil, 4, 0.0},
		{"non-positive k", []string{"a"}, []string{"a"}, 0, 0.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ndcgAtK(tc.ranked, relevantSet(tc.rel), tc.k)
			if !approxEqual(got, tc.want) {
				t.Fatalf("ndcgAtK = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRecallAtK(t *testing.T) {
	cases := []struct {
		name   string
		ranked []string
		rel    []string
		k      int
		want   float64
	}{
		{"two of three relevant in top5", []string{"a", "b", "c", "d", "e"}, []string{"a", "c", "x"}, 5, 2.0 / 3.0},
		{"one of three relevant in top2", []string{"a", "b", "c", "d", "e"}, []string{"a", "c", "x"}, 2, 1.0 / 3.0},
		{"all relevant found", []string{"a", "b"}, []string{"a", "b"}, 5, 1.0},
		{"duplicate hit not double-counted", []string{"a", "a", "b"}, []string{"a", "b"}, 5, 1.0},
		{"empty relevant set", []string{"a"}, nil, 5, 0.0},
		{"non-positive k", []string{"a"}, []string{"a"}, 0, 0.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := recallAtK(tc.ranked, relevantSet(tc.rel), tc.k)
			if !approxEqual(got, tc.want) {
				t.Fatalf("recallAtK = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMRR(t *testing.T) {
	cases := []struct {
		name   string
		ranked []string
		rel    []string
		want   float64
	}{
		{"first relevant at rank 2", []string{"b", "a", "c"}, []string{"a", "c"}, 0.5},
		{"first relevant at rank 1", []string{"a", "b"}, []string{"a"}, 1.0},
		{"first relevant at rank 3", []string{"x", "y", "z"}, []string{"z"}, 1.0 / 3.0},
		{"no relevant found", []string{"x", "y"}, []string{"a"}, 0.0},
		{"empty relevant set", []string{"a"}, nil, 0.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mrr(tc.ranked, relevantSet(tc.rel))
			if !approxEqual(got, tc.want) {
				t.Fatalf("mrr = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRelevantSetDeduplicates(t *testing.T) {
	set := relevantSet([]string{"a", "a", "b"})
	if len(set) != 2 {
		t.Fatalf("relevantSet size = %d, want 2 (deduped)", len(set))
	}
}
