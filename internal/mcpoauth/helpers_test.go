package mcpoauth

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// fakePool is a pool that parses but never dials. pgxpool.New is lazy — it validates the
// DSN and returns without opening a connection — so a store built on it exercises every
// guard that runs BEFORE the query without needing Postgres. The port is 1 so a test that
// accidentally reaches the wire fails fast instead of hanging on a real server.
func fakePool() *pgxpool.Pool {
	pool, err := pgxpool.New(context.Background(), "postgres://u:p@127.0.0.1:1/nowhere?sslmode=disable")
	if err != nil {
		// Unreachable for a constant DSN, and a panic here beats returning nil and
		// having every caller crash later on a nil-pool dereference instead.
		panic("mcpoauth: fake pool DSN no longer parses: " + err.Error())
	}
	return pool
}

// storeForCrypto builds a store whose crypto is real and whose pool is not. Every test
// using it asserts on sealing, on expiry, or on a guard that fires before the query.
func storeForCrypto(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(fakePool(), testSecret)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}
