package tools

import (
	"math"
	"testing"
)

// TestMergeEnvCap is the go/allocation-size-overflow regression for mergeEnv's
// slice capacity hint: parentLen+extraLen+defaults must stay non-negative and
// saturate instead of wrapping when either input approaches maxInt.
func TestMergeEnvCap(t *testing.T) {
	cases := []struct {
		name      string
		parentLen int
		extraLen  int
		want      int
	}{
		{"zero", 0, 0, 2},
		{"typical", 40, 3, 45},
		{"negative_parent", -1, 0, 0},
		{"negative_extra", 0, -1, 0},
		{"overflow_saturates", math.MaxInt - 1, 4, math.MaxInt},
		{"max_parent_saturates", math.MaxInt, 0, math.MaxInt},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mergeEnvCap(tc.parentLen, tc.extraLen); got != tc.want {
				t.Fatalf("mergeEnvCap(%d, %d) = %d, want %d", tc.parentLen, tc.extraLen, got, tc.want)
			}
		})
	}
}
