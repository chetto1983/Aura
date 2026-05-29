package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MigrationRow is one entry from golang-migrate's own `schema_migrations`
// tracking table. The table is created and managed by golang-migrate's
// postgres driver — Aura's `aura.knowledge_migrations` audit table is
// a separate Slice 0.7 concern.
type MigrationRow struct {
	Version int64
	Dirty   bool
}

// Status returns the rows of `schema_migrations` (golang-migrate's own
// tracking table). Returns an empty slice if the table doesn't exist yet
// (first boot before any migrate run).
func Status(ctx context.Context, pool *pgxpool.Pool) ([]MigrationRow, error) {
	if pool == nil {
		return nil, fmt.Errorf("status: pool is nil")
	}
	const q = `
SELECT version, dirty
FROM public.schema_migrations
ORDER BY version
`
	rows, err := pool.Query(ctx, q)
	if err != nil {
		// Table-not-found = no migrations applied yet. Surface a clean empty
		// result instead of a wrapped "relation does not exist" error.
		return []MigrationRow{}, nil //nolint:nilerr // intentional: pre-first-migrate state is not an error
	}
	defer rows.Close()
	out := []MigrationRow{}
	for rows.Next() {
		var r MigrationRow
		if err := rows.Scan(&r.Version, &r.Dirty); err != nil {
			return nil, fmt.Errorf("status scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("status rows: %w", err)
	}
	return out, nil
}
