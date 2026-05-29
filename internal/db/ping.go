package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Ping runs `SELECT 1` against the pool and returns the measured latency.
// Used by `aura db ping` and by integration test TestPing.
func Ping(ctx context.Context, pool *pgxpool.Pool) (time.Duration, error) {
	if pool == nil {
		return 0, fmt.Errorf("ping: pool is nil")
	}
	start := time.Now()
	var one int
	if err := pool.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
		return 0, fmt.Errorf("ping query: %w", err)
	}
	if one != 1 {
		return 0, fmt.Errorf("ping: unexpected result %d", one)
	}
	return time.Since(start), nil
}
