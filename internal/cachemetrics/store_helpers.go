package cachemetrics

import (
	"fmt"
	"strconv"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/pgnumeric"
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
	cost, err := pgnumeric.NumericFromFloat(p.CostUSD)
	if err != nil {
		return sqlc.InsertCacheMetricParams{}, fmt.Errorf("conversation_id %q seq %d: %w", p.ConversationID, p.Seq, err)
	}
	return sqlc.InsertCacheMetricParams{
		ConversationID: convID,
		Seq:            int32(p.Seq),
		PromptTokens:   int32(p.PromptTokens),
		CachedTokens:   int32(p.CachedTokens),
		CostUsd:        cost,
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

// anyInt64 coerces a sqlc `coalesce(sum(int),0)` result to int64. pgx may decode the
// bigint sum as int64 directly, or as a pgtype.Numeric / text depending on the planner;
// this covers each shape so the aggregate is stable across PG versions. An unparseable
// text shape or an unmodeled driver type is an error, NOT a silent 0 — reporting a
// fabricated zero for a cost/cache aggregate is exactly the false-green this codebase
// refuses (WR-02). The float64 branch truncates toward zero, which is safe because token
// sums are integral and exact in float64 up to 2^53 (IN-04).
func anyInt64(v any) (int64, error) {
	switch n := v.(type) {
	case int64:
		return n, nil
	case int32:
		return int64(n), nil
	case int:
		return int64(n), nil
	case float64:
		return int64(n), nil
	case pgtype.Numeric:
		return int64(pgnumeric.FloatFromNumeric(n)), nil
	case string:
		i, err := strconv.ParseInt(n, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("decode int64 aggregate from string %q: %w", n, err)
		}
		return i, nil
	case []byte:
		i, err := strconv.ParseInt(string(n), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("decode int64 aggregate from bytes %q: %w", n, err)
		}
		return i, nil
	default:
		return 0, fmt.Errorf("unmodeled int64 aggregate shape %T", v)
	}
}

// anyNumericFloat coerces a sqlc `coalesce(sum(numeric),0)` result to float64. The cost
// sum decodes as pgtype.Numeric (or text on some drivers); both are handled. As with
// anyInt64, an unparseable text shape or an unmodeled driver type is an error rather than
// a silent 0 — a `$0.00` cost that is actually an unrecognized decode shape would read as
// "cache is working great, free" (WR-02).
func anyNumericFloat(v any) (float64, error) {
	switch n := v.(type) {
	case pgtype.Numeric:
		return pgnumeric.FloatFromNumeric(n), nil
	case float64:
		return n, nil
	case int64:
		return float64(n), nil
	case string:
		f, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return 0, fmt.Errorf("decode float aggregate from string %q: %w", n, err)
		}
		return f, nil
	case []byte:
		f, err := strconv.ParseFloat(string(n), 64)
		if err != nil {
			return 0, fmt.Errorf("decode float aggregate from bytes %q: %w", n, err)
		}
		return f, nil
	default:
		return 0, fmt.Errorf("unmodeled float aggregate shape %T", v)
	}
}
