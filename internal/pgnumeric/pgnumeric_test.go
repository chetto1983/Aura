package pgnumeric

import (
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// TestNumericFromFloat_Characterization pins the numeric(10,4) encoding of the
// canonical helper (QUAL-03). The expected Int+Exp values are the union of inputs the
// two old copies (conversations.numericFromFloat / cachemetrics.numericFromFloat)
// handled identically; only their out-of-range error STRING differed, so this asserts
// the pgtype.Numeric value and err-presence, never the message (Pitfall 5).
func TestNumericFromFloat_Characterization(t *testing.T) {
	cases := []struct {
		name    string
		in      float64
		wantInt int64
		wantExp int32
		wantErr bool
	}{
		{"zero", 0, 0, -numericScale, false},
		{"positive 1.2345", 1.2345, 12345, -numericScale, false},
		{"negative 1.2345", -1.2345, -12345, -numericScale, false},
		{"round half-away 0.12345", 0.12345, 1235, -numericScale, false},
		{"max boundary", DefaultNumericMaxCost, 9999999999, -numericScale, false},
		{"min boundary", -DefaultNumericMaxCost, -9999999999, -numericScale, false},
		{"tiny rounds to zero", 0.00001, 0, -numericScale, false},
		{"over range", 1e9, 0, 0, true},
		{"under range", -1e9, 0, 0, true},
		{"NaN", math.NaN(), 0, 0, true},
		{"positive inf", math.Inf(1), 0, 0, true},
		{"negative inf", math.Inf(-1), 0, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NumericFromFloat(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("NumericFromFloat(%v): want error, got nil", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("NumericFromFloat(%v): unexpected error %v", tc.in, err)
			}
			if !got.Valid || got.Int == nil {
				t.Fatalf("NumericFromFloat(%v): want a valid mantissa, got %+v", tc.in, got)
			}
			if got.Int.Int64() != tc.wantInt {
				t.Errorf("NumericFromFloat(%v) Int = %s, want %d", tc.in, got.Int.String(), tc.wantInt)
			}
			if got.Exp != tc.wantExp {
				t.Errorf("NumericFromFloat(%v) Exp = %d, want %d", tc.in, got.Exp, tc.wantExp)
			}
		})
	}
}

// TestFloatFromNumeric_RoundTripAndNullSafety proves the read-boundary inverse
// round-trips within the 1e-4 scale resolution and that a NULL/NaN numeric reads as 0
// (the union of the two old floatFromNumeric copies' null-safety).
func TestFloatFromNumeric_RoundTripAndNullSafety(t *testing.T) {
	for _, f := range []float64{0, 1.2345, -1.2345, 0.5, 999999.9999, -0.0042} {
		n, err := NumericFromFloat(f)
		if err != nil {
			t.Fatalf("NumericFromFloat(%v): %v", f, err)
		}
		if got := FloatFromNumeric(n); got < f-1e-4 || got > f+1e-4 {
			t.Errorf("round-trip FloatFromNumeric(NumericFromFloat(%v)) = %v, want within 1e-4", f, got)
		}
	}
	if got := FloatFromNumeric(pgtype.Numeric{}); got != 0 {
		t.Errorf("FloatFromNumeric(invalid) = %v, want 0", got)
	}
	if got := FloatFromNumeric(pgtype.Numeric{NaN: true, Valid: true}); got != 0 {
		t.Errorf("FloatFromNumeric(NaN) = %v, want 0", got)
	}
}

// TestFloatFromNumeric_Float64ValueErrorReadsZero covers the read-boundary guard for a
// numeric whose Float64Value() conversion overflows ParseFloat to ErrRange: a
// malformed stored cost reads as 0, never a panic (re-homed here with the helper from
// internal/cachemetrics, QUAL-03).
func TestFloatFromNumeric_Float64ValueErrorReadsZero(t *testing.T) {
	huge, _ := new(big.Int).SetString("1"+strings.Repeat("0", 40), 10)
	n := pgtype.Numeric{Int: huge, Exp: 2147483647, Valid: true}
	if _, err := n.Float64Value(); err == nil {
		t.Skip("this pgtype build does not error on the overflow numeric; branch unreachable here")
	}
	if got := FloatFromNumeric(n); got != 0 {
		t.Errorf("numeric whose Float64Value errors must read as 0, got %v", got)
	}
}
