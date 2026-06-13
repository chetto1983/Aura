//go:build db_integration

// Integration coverage for WithTx — the single transaction seam every
// multi-statement write reuses. Drives a real pool through commit, rollback-on-
// error, panic-rollback-then-repanic, and the Begin-failure early return. Uses
// aura.knowledge_migrations (aura_app has INSERT) with a reserved 91xx version
// band so it never collides with shipped migrations or sibling tests.
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

func countKMVersion(t *testing.T, pool *pgxpool.Pool, version int32) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		"SELECT count(*) FROM aura.knowledge_migrations WHERE version = $1", version).Scan(&n); err != nil {
		t.Fatalf("count version %d: %v", version, err)
	}
	return n
}

func cleanupKM(t *testing.T, pool *pgxpool.Pool, versions ...int32) {
	t.Helper()
	for _, v := range versions {
		if _, err := pool.Exec(context.Background(),
			"DELETE FROM aura.knowledge_migrations WHERE version = $1", v); err != nil {
			t.Logf("cleanup version %d (best-effort): %v", v, err)
		}
	}
}

func TestWithTx_CommitPersists(t *testing.T) {
	pool := appPoolMigrated(t)
	const v int32 = 9101
	cleanupKM(t, pool, v)
	t.Cleanup(func() { cleanupKM(t, pool, v) })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := WithTx(ctx, pool, func(q *sqlc.Queries) error {
		return q.RecordKnowledgeMigration(ctx, sqlc.RecordKnowledgeMigrationParams{
			Version: v, Name: "withtx_commit", Checksum: "c1",
		})
	})
	if err != nil {
		t.Fatalf("WithTx (commit path): %v", err)
	}
	if got := countKMVersion(t, pool, v); got != 1 {
		t.Errorf("after commit: want exactly 1 row at version %d, got %d", v, got)
	}
}

func TestWithTx_RollbackOnError(t *testing.T) {
	pool := appPoolMigrated(t)
	const v int32 = 9102
	cleanupKM(t, pool, v)
	t.Cleanup(func() { cleanupKM(t, pool, v) })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sentinel := errors.New("boom")
	err := WithTx(ctx, pool, func(q *sqlc.Queries) error {
		if e := q.RecordKnowledgeMigration(ctx, sqlc.RecordKnowledgeMigrationParams{
			Version: v, Name: "withtx_rollback", Checksum: "c2",
		}); e != nil {
			return e
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithTx (error path): want sentinel boom, got %v", err)
	}
	if got := countKMVersion(t, pool, v); got != 0 {
		t.Errorf("after rollback-on-error: want 0 rows at version %d, got %d", v, got)
	}
}

func TestWithTx_PanicRollsBackAndRepanics(t *testing.T) {
	pool := appPoolMigrated(t)
	const v int32 = 9103
	cleanupKM(t, pool, v)
	t.Cleanup(func() { cleanupKM(t, pool, v) })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_ = WithTx(ctx, pool, func(q *sqlc.Queries) error {
			_ = q.RecordKnowledgeMigration(ctx, sqlc.RecordKnowledgeMigrationParams{
				Version: v, Name: "withtx_panic", Checksum: "c3",
			})
			panic("kaboom")
		})
	}()
	if recovered == nil {
		t.Fatal("WithTx (panic path): panic was swallowed, want it re-raised")
	}
	if got := countKMVersion(t, pool, v); got != 0 {
		t.Errorf("after panic-rollback: want 0 rows at version %d, got %d", v, got)
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
