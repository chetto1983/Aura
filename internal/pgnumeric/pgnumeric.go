// Package pgnumeric is the canonical leaf for the numeric(10,4) USD-cost conversions
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
// the "Postgres home, not neostore" intent while staying cycle-free (D-06).
package pgnumeric

import (
	"fmt"
	"math"
	"math/big"

	"github.com/jackc/pgx/v5/pgtype"
)

// numericScale is the fixed scale of the cost columns (numeric(10,4)). The cost delta
// is encoded at this scale so the SQL `cost + $delta` stays exact (Pitfall 5).
const numericScale = 4

// DefaultNumericMaxCost is the largest magnitude representable in a numeric(10,4) cost
// column: 6 integer digits + 4 fractional → ±999999.9999. A value outside this range
// cannot be stored without mantissa overflow, so NumericFromFloat rejects it loudly
// rather than silently truncating (WR-01/IN-06).
const DefaultNumericMaxCost = 999999.9999

// NumericFromFloat encodes a USD value as a pgtype.Numeric at numeric(10,4) scale so
// the SQL `cost + $delta` stays exact (Pitfall 5). The value is rounded
// half-away-from-zero to 4 decimals via an integer-mantissa construction
// (Int * 10^-numericScale). NaN/±Inf or a magnitude outside ±DefaultNumericMaxCost is
// an error, never a silently-overflowed mantissa.
func NumericFromFloat(f float64) (pgtype.Numeric, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) || f > DefaultNumericMaxCost || f < -DefaultNumericMaxCost {
		return pgtype.Numeric{}, fmt.Errorf("cost %v out of numeric(10,4) range ±%v", f, DefaultNumericMaxCost)
	}
	scaled := f * 1e4
	if scaled >= 0 {
		scaled += 0.5
	} else {
		scaled -= 0.5
	}
	return pgtype.Numeric{Int: big.NewInt(int64(scaled)), Exp: -numericScale, Valid: true}, nil
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
