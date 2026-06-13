package main

import (
	"math"
	"testing"
)

// TestClampInt64ToInt is the go/incorrect-integer-conversion regression for the
// serve_channels todayCost path: the aggregate's int64 token sum (decoded via
// strconv.ParseInt) must narrow to int without truncation/sign-flip, saturating
// at the int range and flooring negatives to 0.
func TestClampInt64ToInt(t *testing.T) {
	cases := []struct {
		name string
		in   int64
		want int
	}{
		{"zero", 0, 0},
		{"typical", 12345, 12345},
		{"negative_floors_to_zero", -1, 0},
		{"min_int64_floors_to_zero", math.MinInt64, 0},
		{"max_int_passes_through", math.MaxInt, math.MaxInt},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clampInt64ToInt(tc.in); got != tc.want {
				t.Fatalf("clampInt64ToInt(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}
