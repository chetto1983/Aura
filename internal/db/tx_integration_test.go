//go:build db_integration

// Integration coverage for WithTx — the single transaction seam every
// multi-statement write reuses. Drives a real pool through commit, rollback-on-
// error, panic-rollback-then-repanic, and the Begin-failure early return.
//
// It writes aura.mcp_audit under a reserved `withtx-<case>` server_name. It used
// aura.knowledge_migrations until migration 0084 retired that table with the Cypher
// migration ledger, which left these three tests failing on SQLSTATE 42P01.
//
// mcp_audit is the replacement for a reason and not by luck: WithTx is the PLAIN seam,
// the one that sets no identity, so the table under it must be one RLS does not scope —
// every scoped table would refuse the insert and the test would prove the policy instead
// of the transaction.
//
// It is APPEND-ONLY (a trigger rejects DELETE regardless of the grant), so these tests
// assert a DELTA around each call rather than an absolute count and clean up nothing. An
// absolute count would pass once and fail on every rerun — which is exactly what the
// first attempt did.
//
// Run via: go test -tags db_integration ./internal/db -run TestWithTx -count=1

package db

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgxpool"
)

// appPoolMigrated ensures roles + migrations exist, then opens an aura_app pool.
func appPoolMigrated(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	migrateURL := envOrSkip(t, "AURA_DB_MIGRATE_URL")
	if err := EnsureRoles(ctx, bootstrapURL(t), os.Getenv("POSTGRES_PASSWORD")); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := Open(ctx, &Config{URL: envOrSkip(t, "AURA_DB_URL")})
	if err != nil {
		t.Fatalf("Open aura_app: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func countAuditRows(t *testing.T, pool *pgxpool.Pool, marker string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		"SELECT count(*) FROM aura.mcp_audit WHERE server_name = $1", marker).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", marker, err)
	}
	return n
}

// auditRow is the one write these tests make: a reserved marker in server_name, a fixed
// actor, nothing that any other test reads.
func auditRow(marker string) sqlc.InsertMcpAuditParams {
	return sqlc.InsertMcpAuditParams{
		ActorIdentityID: "withtx-integration",
		Action:          "probe",
		ServerName:      marker,
	}
}

func TestWithTx_CommitPersists(t *testing.T) {
	pool := appPoolMigrated(t)
	const marker = "withtx-commit"
	before := countAuditRows(t, pool, marker)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := WithTx(ctx, pool, func(q *sqlc.Queries) error {
		_, e := q.InsertMcpAudit(ctx, auditRow(marker))
		return e
	})
	if err != nil {
		t.Fatalf("WithTx (commit path): %v", err)
	}
	if got := countAuditRows(t, pool, marker) - before; got != 1 {
		t.Errorf("commit added %d rows for %s, want exactly 1", got, marker)
	}
}

func TestWithTx_RollbackOnError(t *testing.T) {
	pool := appPoolMigrated(t)
	const marker = "withtx-rollback"
	before := countAuditRows(t, pool, marker)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sentinel := errors.New("boom")
	err := WithTx(ctx, pool, func(q *sqlc.Queries) error {
		if _, e := q.InsertMcpAudit(ctx, auditRow(marker)); e != nil {
			return e
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithTx (error path): want sentinel boom, got %v", err)
	}
	if got := countAuditRows(t, pool, marker) - before; got != 0 {
		t.Errorf("rollback-on-error left %d rows for %s, want 0", got, marker)
	}
}

func TestWithTx_PanicRollsBackAndRepanics(t *testing.T) {
	pool := appPoolMigrated(t)
	const marker = "withtx-panic"
	before := countAuditRows(t, pool, marker)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_ = WithTx(ctx, pool, func(q *sqlc.Queries) error {
			_, _ = q.InsertMcpAudit(ctx, auditRow(marker))
			panic("kaboom")
		})
	}()
	if recovered == nil {
		t.Fatal("WithTx (panic path): panic was swallowed, want it re-raised")
	}
	if got := countAuditRows(t, pool, marker) - before; got != 0 {
		t.Errorf("panic-rollback left %d rows for %s, want 0", got, marker)
	}
}

func TestWithTx_BeginErrorOnClosedPool(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := Open(ctx, &Config{URL: envOrSkip(t, "AURA_DB_URL")})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	pool.Close() // Begin on a closed pool fails — exercises WithTx's early return.

	called := false
	err = WithTx(ctx, pool, func(*sqlc.Queries) error { called = true; return nil })
	if err == nil {
		t.Fatal("WithTx on closed pool: want a Begin error, got nil")
	}
	if called {
		t.Error("WithTx: fn must not run when Begin fails")
	}
}
