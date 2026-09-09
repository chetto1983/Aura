// Package pgnumeric is the canonical leaf for the numeric(24,12) USD-cost conversions
// shared by the Postgres-backed cost stores: conversations.total_cost_usd and
// cache_metrics.cost_usd. Before this extraction the byte-identical
// numericFromFloat/floatFromNumeric were copied across internal/conversations and
// internal/cachemetrics (QUAL-03); the two copies differed only in their out-of-range
// error string.
//
// It is a stdlib + pgtype leaf (no internal imports) so both stores can depend on the
// seam without an import cycle. The home is deliberately NOT internal/db (Open
// Question #1's first instinct): internal/db's cache_metrics integration test is
// package db and imports internal/cachemetrics, so making cachemetrics import
// internal/db forms a db↔cachemetrics test cycle. A dedicated pg-flavoured leaf keeps
// the "Postgres-owned numeric seam" intent while staying cycle-free (D-06).
//
// Widened from numeric(10,4) to numeric(24,12) by phase 02 plan 07's checkpoint
// (T-02-36): migration NNNN widens the two COLUMNS, but the column type alone is not
// the fix — this file is the Go-side encoder that sat between every write and those
// columns, and it hardcoded scale 4 regardless of what the column could hold. A
// per-call cost of 0.000004158 (measured live, 02-CONTEXT.md M-09) was rounding to
// 0.0000 HERE, in NumericFromFloat, before the value ever reached the widened column —
// widening the column without this change would have left the exact defect the
// checkpoint exists to fix. Discovered by reading this file before editing it
// (CLAUDE.md's NEVER SUPPOSE), not assumed from the checkpoint's own "no code change
// needed" blast-radius note, which this correction disproves.
package pgnumeric

import (
	"fmt"
	"math"
	"math/big"

	"github.com/jackc/pgx/v5/pgtype"
)

// numericScale is the fixed scale of the cost columns (numeric(24,12)). The cost delta
// is encoded at this scale so the SQL `cost + $delta` stays exact (Pitfall 5).
const numericScale = 12

// numericScaleFactor is 10^numericScale, the fractional-part multiplier. Kept as its
// own constant so NumericFromFloat's split below never re-derives it inline.
const numericScaleFactor = 1e12

// DefaultNumericMaxCost is the largest magnitude NumericFromFloat accepts: 6 integer
// digits (±999999) plus fractional headroom, unchanged in DOLLAR magnitude from the
// pre-widening ±999999.9999 bound — the widening buys fractional precision (4 decimals
// to 12), not a larger dollar ceiling, since no real per-call or per-conversation cost
// approaches even the OLD bound. A value outside this range is rejected loudly rather
// than silently truncated (WR-01/IN-06).
const DefaultNumericMaxCost = 999999.999999

// NumericFromFloat encodes a USD value as a pgtype.Numeric at numeric(24,12) scale so
// the SQL `cost + $delta` stays exact (Pitfall 5) and a value as small as the measured
// 0.000004158 (six significant digits into the fraction) survives instead of rounding
// to zero.
//
// The mantissa is built from an INTEGER/FRACTIONAL split (math.Modf), not a single
// `f * 1e12` multiply: the integer part (at most 999999) is exact in float64 and is
// multiplied into a big.Int, which has no magnitude ceiling; the fractional part is
// always < 1, so scaling IT by 1e12 never leaves float64's exact-integer range either
// (< 1e12, far inside the 2^53 ceiling). Multiplying the WHOLE value by 1e12 directly —
// the scale-4 predecessor's approach, safe at that scale because 1e10 stayed under
// 2^53 — silently loses precision once the multiplier is 1e12 and the value has six
// integer digits (999999 * 1e12 already exceeds 2^53 by two orders of magnitude); the
// split sidesteps that ceiling instead of hoping the product happens to be exact.
//
// Rounding stays half-away-from-zero, replicated on the (always non-negative, sign
// factored out first) fractional scale exactly as the scale-4 version applied it
// symmetrically on the signed whole.
func NumericFromFloat(f float64) (pgtype.Numeric, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) || f > DefaultNumericMaxCost || f < -DefaultNumericMaxCost {
		return pgtype.Numeric{}, fmt.Errorf("cost %v out of numeric(24,%d) range ±%v", f, numericScale, DefaultNumericMaxCost)
	}
	neg := f < 0
	if neg {
		f = -f
	}
	whole, frac := math.Modf(f)
	fracScaled := frac*numericScaleFactor + 0.5 // frac >= 0 here; half-away-from-zero.
	mantissa := new(big.Int).Mul(big.NewInt(int64(whole)), big.NewInt(int64(numericScaleFactor)))
	mantissa.Add(mantissa, big.NewInt(int64(fracScaled)))
	if neg {
		mantissa.Neg(mantissa)
	}
	return pgtype.Numeric{Int: mantissa, Exp: -numericScale, Valid: true}, nil
}

// FloatFromNumeric converts a pgtype.Numeric cost column to float64 at the read
// boundary. An invalid/NULL/NaN numeric reads as 0.
func FloatFromNumeric(n pgtype.Numeric) float64 {
	if !n.Valid || n.NaN {
		return 0
	}
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return 0
	}
	return f.Float64
}
