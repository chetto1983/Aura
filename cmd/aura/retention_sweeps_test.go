package main

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/channels/telegram"
	"github.com/chetto1983/aura/internal/idempotency"
)

// A deployment without a pool registers neither sweep: a bare nil, which the retention
// handler skips, never a store whose queries would dereference a nil pool. With one, each
// builder hands the retention job its store (pgxpool.New opens no connection).
func TestRetentionSweepBuildersNeedAPool(t *testing.T) {
	if sweep := buildTelegramTokenSweep(&chatEnv{}); sweep != nil {
		t.Fatalf("no pool built a token sweep %#v, want a bare nil", sweep)
	}
	if sweep := buildReplayBodySweep(&chatEnv{}); sweep != nil {
		t.Fatalf("no pool built a replay sweep %#v, want a bare nil", sweep)
	}
	pool, err := pgxpool.New(context.Background(), "postgres://unused:unused@127.0.0.1:1/unused")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, ok := buildTelegramTokenSweep(&chatEnv{pool: pool}).(*telegram.Store); !ok {
		t.Fatal("a pool must build the telegram store as the token sweep")
	}
	if _, ok := buildReplayBodySweep(&chatEnv{pool: pool}).(*idempotency.Store); !ok {
		t.Fatal("a pool must build the idempotency store as the replay sweep")
	}
}
