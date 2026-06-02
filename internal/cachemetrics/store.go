// Package cachemetrics is the per-domain Store over aura.cache_metrics (PRD Slice 4 /
// Phase 6, D-02). It copies the canonical Store{pool,q} pattern proven in
// internal/identity and reused by internal/conversations (D-A4-01): a thin wrapper over
// the generated sqlc surface with pgtype boundary conversion (Pitfall 5) and %w error
// wrapping. No interface is declared here — the consumer (internal/runner) declares the
// narrow CacheMetricStore it needs (D-A2-02, "accept interfaces, return structs").
//
// Scope: one append-only INSERT per completed assistant turn (Insert, written from the
// already-computed llm.Usage in runner_persist.go, D-02a) plus the two time-windowed
// reads consumed by `aura cache-stats --since=<window>` in 06-04 (ListSince + AggregateSince).
package cachemetrics

import (
	"context"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// numericScale is the fixed scale of cost_usd (numeric(10,4)), matching the column DDL.
const numericScale = 4

// Store wraps a pgx pool and the generated Queries — the canonical shape from
// internal/identity / internal/conversations.
type Store struct {
	q *sqlc.Queries
}

// New builds a Store over an open pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{q: sqlc.New(pool)}
}

// Insert appends one immutable cache-metric row for a completed assistant turn. The
// caller passes the already-computed sqlc.InsertCacheMetricParams (the Runner reuses the
// per-turn llm.Usage + cost it has in hand — no wire-path touch, D-02a).
func (s *Store) Insert(ctx context.Context, p sqlc.InsertCacheMetricParams) error {
	if err := s.q.InsertCacheMetric(ctx, p); err != nil {
		return fmt.Errorf("insert cache metric: %w", err)
	}
	return nil
}

// Metric is the domain projection of one aura.cache_metrics row — plain Go types at the
// package boundary instead of the sqlc pgtype wrappers.
type Metric struct {
	ConversationID string
	Seq            int
	TS             time.Time
	PromptTokens   int
	CachedTokens   int
	CostUSD        float64
}

// ListSince returns the metric rows with ts >= since, in ts ASC order (the query window
// is parameterized; since is bound, never interpolated — T-06-02).
func (s *Store) ListSince(ctx context.Context, since time.Time) ([]Metric, error) {
	rows, err := s.q.ListCacheMetricsSince(ctx, timestamptzFrom(since))
	if err != nil {
		return nil, fmt.Errorf("list cache metrics since %s: %w", since.Format(time.RFC3339), err)
	}
	out := make([]Metric, 0, len(rows))
	for _, r := range rows {
		out = append(out, Metric{
			ConversationID: uuid.UUID(r.ConversationID.Bytes).String(),
			Seq:            int(r.Seq),
			TS:             r.Ts.Time,
			PromptTokens:   int(r.PromptTokens),
			CachedTokens:   int(r.CachedTokens),
			CostUSD:        floatFromNumeric(r.CostUsd),
		})
	}
	return out, nil
}

// Aggregate is the windowed roll-up of cache_metrics. The hit-rate ratio is left to the
// caller (06-04) so the divide-by-zero guard (TotalPromptTokens==0 → ratio 0) lives at
// the presentation boundary, not in SQL.
type Aggregate struct {
	Turns             int64
	TotalPromptTokens int64
	TotalCachedTokens int64
	TotalCostUSD      float64
}

// AggregateSince sums prompt/cached tokens + cost over the ts >= since window (SQL-side
// aggregation; the parameterized since is bound, T-06-02).
func (s *Store) AggregateSince(ctx context.Context, since time.Time) (Aggregate, error) {
	row, err := s.q.AggregateCacheMetricsSince(ctx, timestamptzFrom(since))
	if err != nil {
		return Aggregate{}, fmt.Errorf("aggregate cache metrics since %s: %w", since.Format(time.RFC3339), err)
	}
	return Aggregate{
		Turns:             row.Turns,
		TotalPromptTokens: anyInt64(row.TotalPromptTokens),
		TotalCachedTokens: anyInt64(row.TotalCachedTokens),
		TotalCostUSD:      anyNumericFloat(row.TotalCostUsd),
	}, nil
}
