package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
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

// undefinedTable is SQLSTATE 42P01 (relation does not exist) — the table has not
// been created yet because no migration has run on this database.
const undefinedTable = "42P01"

// isUndefinedTable reports whether err is Postgres SQLSTATE 42P01. pgx executes
// pool.Query lazily, so a missing-table error can surface either at Query time or
// during row iteration (rows.Err); both call sites funnel through here.
func isUndefinedTable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == undefinedTable
}

// Status returns the rows of `schema_migrations` (golang-migrate's own tracking
// table). A genuinely missing table (pre-first-migrate, SQLSTATE 42P01) yields an
// empty slice and nil error — that is the documented first-boot contract. Any
// other failure (e.g. 42501 insufficient_privilege when the connecting role lacks
// SELECT on the tracker) is surfaced as a wrapped error rather than masked.
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
		if isUndefinedTable(err) {
			return []MigrationRow{}, nil
		}
		return nil, fmt.Errorf("status query: %w", err)
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
		if isUndefinedTable(err) {
			return []MigrationRow{}, nil
		}
		return nil, fmt.Errorf("status rows: %w", err)
	}
	return out, nil
}
