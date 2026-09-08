//go:build db_integration

package steer

import (
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestExpiredWorkerCorrectionDoesNotEnterParentHistory(t *testing.T) {
	pool := steerDisposablePool(t)
	owner, conv := seedIdentityAndConversation(t, pool)
	ctx := workerSteerContext(t, owner, "expires")
	store := NewPostgresStore(pool, Config{Max: 8, MaxBytes: 4096, SteerTTL: time.Millisecond})
	if err := store.PushWorker(ctx, conv, "w1", "run-"+uuid.NewString(), "instruction for the child only"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	sweeper := NewSweeper(pool, conversations.New(pool, conversations.Config{}))
	if count, err := sweeper.ExpireDue(ctx, time.Now(), 100); err != nil || count != 1 {
		t.Fatalf("expiry: count=%d err=%v", count, err)
	}
	var parentTurns int
	if err := db.WithIdentityTxRaw(ctx, pool, owner, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT count(*) FROM aura.conversation_turns WHERE conversation_id = $1", conv).Scan(&parentTurns)
	}); err != nil {
		t.Fatal(err)
	}
	if parentTurns != 0 {
		t.Fatalf("expired child instruction leaked into %d parent history turns", parentTurns)
	}
	history, err := store.WorkerHistory(ctx, conv, "w1")
	if err != nil || len(history) != 1 || history[0].Status != "rejected" {
		t.Fatalf("worker receipt must retain the rejection: %+v %v", history, err)
	}
}
