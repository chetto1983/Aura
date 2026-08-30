//go:build db_integration

package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/steer"
	"github.com/chetto1983/aura/internal/swarm"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestDelegationFanoutClaimRollsBackWithoutOutbox(t *testing.T) {
	pool := mcpAuditMigratedPool(t)
	identityID, conversationID := uuid.NewString(), uuid.NewString()
	if _, err := pool.Exec(t.Context(),
		"INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')",
		identityID, "fanout-outbox-"+identityID[:8]); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	if err := db.WithIdentityTxRaw(t.Context(), pool, identityID, func(tx pgx.Tx) error {
		_, err := tx.Exec(t.Context(),
			"INSERT INTO aura.conversations (id, identity_id, model, metadata) VALUES ($1, $2, 'test', '{}'::jsonb)",
			conversationID, identityID)
		return err
	}); err != nil {
		t.Fatalf("seed conversation: %v", err)
	}

	steerStore := steer.NewPostgresStore(pool, steer.Config{Max: 8, MaxBytes: 16384, DelegationResultTTL: time.Hour})
	if err := steerStore.PushDelegationResult(conversationID, steer.SourceWorker, `[{"goal_index":0,"status":"ok"}]`, "f-atomic"); err != nil {
		t.Fatalf("seed delegation result: %v", err)
	}
	rows, err := steerStore.ListUnnudgedDelegationResults(t.Context(), time.Now().Add(time.Minute), 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list candidates = %d, %v", len(rows), err)
	}
	candidates := []swarm.UndrainedResult{{
		ID: rows[0].ID, IdentityID: rows[0].IdentityID, ConversationID: rows[0].ConversationID,
		Body: rows[0].Body, FanoutKey: rows[0].FanoutKey,
	}}
	adapter := delegationPendingNotifier{pool: pool, store: cron.New(pool)}
	buildErr := errors.New("render failed after claim")
	if _, _, err := adapter.ClaimFanoutNotification(context.Background(), candidates, func([]swarm.UndrainedResult) (string, error) {
		return "", buildErr
	}); !errors.Is(err, buildErr) {
		t.Fatalf("failed claim error = %v, want %v", err, buildErr)
	}

	var nudged bool
	var pending int
	if err := pool.QueryRow(t.Context(),
		"SELECT nudged_at IS NOT NULL FROM aura.steer_queue WHERE id = $1", rows[0].ID).Scan(&nudged); err != nil {
		t.Fatalf("read claim state: %v", err)
	}
	if err := pool.QueryRow(t.Context(),
		"SELECT count(*) FROM aura.pending_notifications WHERE steer_queue_id = $1", rows[0].ID).Scan(&pending); err != nil {
		t.Fatalf("read outbox state: %v", err)
	}
	if nudged || pending != 0 {
		t.Fatalf("rolled-back claim/outbox = nudged %v pending %d, want false/0", nudged, pending)
	}

	notification, claimed, err := adapter.ClaimFanoutNotification(t.Context(), candidates, func([]swarm.UndrainedResult) (string, error) {
		return "rendered fanout", nil
	})
	if err != nil || !claimed || notification.Body != "rendered fanout" {
		t.Fatalf("successful claim = %+v, %v, %v", notification, claimed, err)
	}
	var status string
	if err := pool.QueryRow(t.Context(), `
		SELECT count(*), min(status)
		FROM aura.pending_notifications
		WHERE steer_queue_id = $1`, rows[0].ID).Scan(&pending, &status); err != nil {
		t.Fatalf("read committed outbox: %v", err)
	}
	if err := pool.QueryRow(t.Context(),
		"SELECT nudged_at IS NOT NULL FROM aura.steer_queue WHERE id = $1", rows[0].ID).Scan(&nudged); err != nil {
		t.Fatalf("read committed claim state: %v", err)
	}
	if !nudged || pending != 1 || status != "pending" {
		t.Fatalf("committed claim/outbox = nudged %v pending %d status %q, want true/1/pending", nudged, pending, status)
	}
	if _, claimed, err = adapter.ClaimFanoutNotification(t.Context(), candidates, func([]swarm.UndrainedResult) (string, error) {
		return "duplicate", nil
	}); err != nil || claimed {
		t.Fatalf("second claim = %v, %v, want false/nil", claimed, err)
	}
	if err := pool.QueryRow(t.Context(),
		"SELECT count(*) FROM aura.pending_notifications WHERE steer_queue_id = $1", rows[0].ID).Scan(&pending); err != nil {
		t.Fatalf("read deduplicated outbox: %v", err)
	}
	if pending != 1 {
		t.Fatalf("outbox rows after second claim = %d, want 1", pending)
	}
}
