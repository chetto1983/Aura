package reasoningtrace

import (
	"math"
	"testing"
)

// TestTraceRowCap is the go/allocation-size-overflow regression for Record's row
// map allocation: the fieldCount+fixed capacity hint must stay non-negative and
// saturate instead of wrapping when the count approaches maxInt.
func TestTraceRowCap(t *testing.T) {
	cases := []struct {
		name string
		in   int
		want int
	}{
		{"zero", 0, 4},
		{"typical", 6, 10},
		{"negative_returns_fixed", -1, 4},
		{"overflow_saturates", math.MaxInt - 1, math.MaxInt},
		{"max_saturates", math.MaxInt, math.MaxInt},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := traceRowCap(tc.in); got != tc.want {
				t.Fatalf("traceRowCap(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}
