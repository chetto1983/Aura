//go:build spike_casbin

package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// dropSpikeTable removes the spike-created table so the run is idempotent and the
// live Aura schema is left untouched.
func dropSpikeTable(ctx context.Context, pool *pgxpool.Pool) {
	_, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS aura.casbin_rule`)
}

// countTable returns how many tables named casbin_rule exist in the aura schema.
func countTable(ctx context.Context, pool *pgxpool.Pool) int {
	var n int
	_ = pool.QueryRow(ctx,
		`SELECT count(*) FROM information_schema.tables WHERE table_schema='aura' AND table_name='casbin_rule'`,
	).Scan(&n)
	return n
}

// columnExists reports whether aura.casbin_rule has the named column.
func columnExists(ctx context.Context, pool *pgxpool.Pool, col string) bool {
	var n int
	_ = pool.QueryRow(ctx,
		`SELECT count(*) FROM information_schema.columns WHERE table_schema='aura' AND table_name='casbin_rule' AND column_name=$1`,
		col,
	).Scan(&n)
	return n == 1
}

// countRows returns the number of rows of the given p_type ('p' or 'g').
func countRows(ctx context.Context, pool *pgxpool.Pool, ptype string) int {
	var n int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM aura.casbin_rule WHERE p_type=$1`, ptype).Scan(&n)
	return n
}
