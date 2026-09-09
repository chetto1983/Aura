package pgnumeric

import (
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

// TestNumericFromFloat_Characterization pins the numeric(24,12) encoding of the
// canonical helper (QUAL-03, widened by phase 02 plan 07's T-02-36 checkpoint). Small,
// exactly-float64-representable inputs assert an EXACT mantissa; the two boundary
// cases assert only Valid/Exp plus a round-trip tolerance, because DefaultNumericMaxCost
// itself is a repeating-binary-fraction literal whose OWN float64 representation is
// already inexact at the 12th decimal — asserting a hardcoded mantissa there would pin
// the Go compiler's literal rounding, not this function's behavior.
func TestNumericFromFloat_Characterization(t *testing.T) {
	cases := []struct {
		name    string
		in      float64
		wantInt int64
		wantExp int32
		wantErr bool
	}{
		{"zero", 0, 0, -numericScale, false},
		{"positive 1.2345", 1.2345, 1234500000000, -numericScale, false},
		{"negative 1.2345", -1.2345, -1234500000000, -numericScale, false},
		{"round half-away 0.12345", 0.12345, 123450000000, -numericScale, false},
		// The M-09 measured per-call cost (02-CONTEXT.md): at the OLD scale-4 encoder
		// this rounded to 0.0000. This is the assertion the whole widening exists for.
		{"M-09 measured per-call cost 0.000004158", 0.000004158, 4158000, -numericScale, false},
		{"tiny rounds to zero at scale 12", 0.0000000000004, 0, -numericScale, false},
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

// TestNumericFromFloat_BoundaryRoundTrips proves the two out-of-band-adjacent boundary
// values (±DefaultNumericMaxCost) encode successfully at the new scale and round-trip
// within the float64 precision the boundary CONSTANT itself already carries — a
// tolerance-based assertion, not an exact mantissa, for the reason the characterization
// test's doc comment states.
func TestNumericFromFloat_BoundaryRoundTrips(t *testing.T) {
	for _, f := range []float64{DefaultNumericMaxCost, -DefaultNumericMaxCost} {
		got, err := NumericFromFloat(f)
		if err != nil {
			t.Fatalf("NumericFromFloat(%v): unexpected error %v", f, err)
		}
		if !got.Valid || got.Exp != -numericScale {
			t.Fatalf("NumericFromFloat(%v) = %+v, want Valid Exp=%d", f, got, -numericScale)
		}
		if round := FloatFromNumeric(got); round < f-1e-6 || round > f+1e-6 {
			t.Errorf("NumericFromFloat(%v) round-trip = %v, want within 1e-6", f, round)
		}
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
