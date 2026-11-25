package main

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/channels/telegram"
)

// A deployment without a pool registers no token sweep: a bare nil, which the retention
// handler skips, never a *telegram.Store whose queries would dereference a nil pool.
func TestBuildTelegramTokenSweepNeedsAPool(t *testing.T) {
	if sweep := buildTelegramTokenSweep(&chatEnv{}); sweep != nil {
		t.Fatalf("no pool built sweep %#v, want a bare nil", sweep)
	}
	pool, err := pgxpool.New(context.Background(), "postgres://unused:unused@127.0.0.1:1/unused")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, ok := buildTelegramTokenSweep(&chatEnv{pool: pool}).(*telegram.Store); !ok {
		t.Fatal("a pool must build the telegram store as the sweep")
	}
}
