package cachemetrics

import (
	"fmt"
	"math/big"
	"strconv"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// MetricParams is the plain-Go input the Runner passes when persisting a turn's metrics.
// It mirrors the AppendTurn ergonomics in internal/conversations: callers work in native
// Go types and NewInsertParams handles the pgtype boundary conversion (Pitfall 5).
type MetricParams struct {
	ConversationID string
	Seq            int
	PromptTokens   int
	CachedTokens   int
	CostUSD        float64
}

// NewInsertParams converts plain-Go MetricParams into the generated sqlc params the
// narrow CacheMetricStore.Insert consumes. Centralizing the pgtype conversion here keeps
// the runner persist seam a one-liner and the conversion logic un-duplicated (D-A4-01).
func NewInsertParams(p MetricParams) (sqlc.InsertCacheMetricParams, error) {
	convID, err := uuidFrom("conversation_id", p.ConversationID)
	if err != nil {
		return sqlc.InsertCacheMetricParams{}, err
	}
	return sqlc.InsertCacheMetricParams{
		ConversationID: convID,
		Seq:            int32(p.Seq),
		PromptTokens:   int32(p.PromptTokens),
		CachedTokens:   int32(p.CachedTokens),
		CostUsd:        numericFromFloat(p.CostUSD),
	}, nil
}

// timestamptzFrom wraps a Go time.Time into the pgtype.Timestamptz the window queries
// bind (the since arg). A zero time is still Valid (epoch) — callers pass a real window.
func timestamptzFrom(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// uuidFrom parses a canonical UUID string into pgtype.UUID (mirrors conversations.parseUUID).
func uuidFrom(field, s string) (pgtype.UUID, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("invalid %s %q: %w", field, s, err)
	}
	return pgtype.UUID{Bytes: u, Valid: true}, nil
}

// numericFromFloat encodes a USD value as a pgtype.Numeric at numeric(10,4) scale so the
// stored cost stays exact (Pitfall 5; mirrors conversations.numericFromFloat). The value
// is rounded half-away-from-zero to 4 decimals via an integer-mantissa construction.
func numericFromFloat(f float64) pgtype.Numeric {
	scaled := f * 1e4
	if scaled >= 0 {
		scaled += 0.5
	} else {
		scaled -= 0.5
	}
	return pgtype.Numeric{Int: big.NewInt(int64(scaled)), Exp: -numericScale, Valid: true}
}

// floatFromNumeric converts a pgtype.Numeric (cost_usd) to float64 at the read boundary.
// An invalid/NULL/NaN numeric reads as 0 (mirrors conversations.floatFromNumeric).
func floatFromNumeric(n pgtype.Numeric) float64 {
	if !n.Valid || n.NaN {
		return 0
	}
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return 0
	}
	return f.Float64
}

// anyInt64 coerces a sqlc `coalesce(sum(int),0)` result to int64. pgx may decode the
// bigint sum as int64 directly, or as a pgtype.Numeric / text depending on the planner;
// this covers each shape so the aggregate is stable across PG versions.
func anyInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int32:
		return int64(n)
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case pgtype.Numeric:
		return int64(floatFromNumeric(n))
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	case []byte:
		i, _ := strconv.ParseInt(string(n), 10, 64)
		return i
	default:
		return 0
	}
}

// anyNumericFloat coerces a sqlc `coalesce(sum(numeric),0)` result to float64. The cost
// sum decodes as pgtype.Numeric (or text on some drivers); both are handled.
func anyNumericFloat(v any) float64 {
	switch n := v.(type) {
	case pgtype.Numeric:
		return floatFromNumeric(n)
	case float64:
		return n
	case int64:
		return float64(n)
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	case []byte:
		f, _ := strconv.ParseFloat(string(n), 64)
		return f
	default:
		return 0
	}
}
