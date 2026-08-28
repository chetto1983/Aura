//go:build db_integration

// delegation_delivery_db_test.go proves the absent-operator leg's
// DB-dependent claims against a REAL Postgres connection: a drained row is
// never nudged (the SQL WHERE clause, not application logic, is what
// excludes it), and two concurrent NudgeUndrained passes over the SAME
// undrained row push at most once (the claim-before-push conditional
// UPDATE, migration 0103's nudged_at). Runs as aura_app via
// delegationDisposablePool (delegation_queue_test.go), never the superuser
// aura role, which would give a false green on the identity-scoping
// assertions steer.PostgresStore's own queries make.
package swarm

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/steer"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// seedDelegationDeliveryConversation seeds one identity + one conversation it
// owns, visible to aura.conversation_owner() (migration 0103), mirroring
// internal/steer/integration_pool_helper_test.go's own
// seedIdentityAndConversation -- copied rather than cross-imported (test-only
// symbols are unexported).
func seedDelegationDeliveryConversation(t *testing.T, pool *pgxpool.Pool) (identityID, conversationID string) {
	t.Helper()
	ctx := context.Background()
	identityID = seedSwarmTestIdentity(t, ctx, pool)
	conversationID = uuid.NewString()
	if err := db.WithIdentityTxRaw(ctx, pool, identityID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			"INSERT INTO aura.conversations (id, identity_id, model, metadata) VALUES ($1, $2, 'test-model', '{}'::jsonb)",
			conversationID, identityID,
		)
		return err
	}); err != nil {
		t.Fatalf("seed conversation: %v", err)
	}
	return identityID, conversationID
}

// dbNudgeAdapter wraps *steer.PostgresStore onto SteerNudgeStore -- the SAME
// translation cmd/aura/serve_delegation.go's own steerNudgeAdapter performs,
// copied here so this proof runs against the REAL steer_queue SQL rather
// than a fake.
type dbNudgeAdapter struct {
	store *steer.PostgresStore
}

func (a dbNudgeAdapter) ListUnnudgedDelegationResults(ctx context.Context, cutoff time.Time, limit int) ([]UndrainedResult, error) {
	rows, err := a.store.ListUnnudgedDelegationResults(ctx, cutoff, limit)
	if err != nil {
		return nil, err
	}
	out := make([]UndrainedResult, 0, len(rows))
	for _, r := range rows {
		out = append(out, UndrainedResult{ID: r.ID, IdentityID: r.IdentityID, Body: r.Body})
	}
	return out, nil
}

func (a dbNudgeAdapter) MarkSteerRowNudged(ctx context.Context, id, identityID string) (bool, error) {
	return a.store.MarkSteerRowNudged(ctx, id, identityID)
}

// channelCounterFunc adapts a plain func() into ChannelDeliverer for the
// concurrency test below -- fakeChannelDeliverer's un-mutexed slice append
// (delegation_delivery_test.go) is not safe for concurrent callers.
type channelCounterFunc func()

func (f channelCounterFunc) DeliverToIdentity(_ context.Context, _, _ string) (bool, error) {
	f()
	return true, nil
}

// TestNudgeSkipsDrained proves a drained delegation_result row is never
// nudged: the operator was present and the steer rail already delivered it
// mid-turn (D-04), so telling them again on their channel would be a
// duplicate notification -- the exact thing SWARM-03's edge forbids.
func TestNudgeSkipsDrained(t *testing.T) {
	pool := delegationDisposablePool(t)
	_, convID := seedDelegationDeliveryConversation(t, pool)
	store := steer.NewPostgresStore(pool, steer.Config{Max: 8, MaxBytes: 16384})

	if err := store.Push(convID, steer.SourceWorker, "the report"); err != nil {
		t.Fatalf("Push: %v", err)
	}
	// The operator's turn drains it -- the present-operator rail already
	// delivered it (D-04), so it must never be nudged.
	if msgs := store.Drain(convID); len(msgs) != 1 {
		t.Fatalf("Drain = %d messages, want 1 (seeding the drained state)", len(msgs))
	}

	channel := &fakeChannelDeliverer{delivered: true}
	d := &DelegationDelivery{Channel: channel, Nudge: dbNudgeAdapter{store: store}, NudgeAfter: time.Minute}

	n, err := d.NudgeUndrained(context.Background(), time.Now().Add(time.Hour), 10)
	if err != nil {
		t.Fatalf("NudgeUndrained: %v", err)
	}
	if n != 0 {
		t.Fatalf("nudged = %d, want 0 -- a drained row must never be nudged", n)
	}
	if len(channel.calls) != 0 {
		t.Fatalf("channel calls = %+v, want none for a drained row", channel.calls)
	}
}

// TestNudgeOnceUnderConcurrency proves the SWARM-09 edge: two sweep passes
// racing over the SAME undrained row push at most once. MarkSteerRowNudged's
// conditional UPDATE (WHERE nudged_at IS NULL) is the idempotency key --
// claim happens BEFORE push (DelegationDelivery.nudgeOne's own doc), so the
// loser of the race never calls the channel at all.
func TestNudgeOnceUnderConcurrency(t *testing.T) {
	pool := delegationDisposablePool(t)
	_, convID := seedDelegationDeliveryConversation(t, pool)
	store := steer.NewPostgresStore(pool, steer.Config{Max: 8, MaxBytes: 16384})

	if err := store.Push(convID, steer.SourceWorker, "the report"); err != nil {
		t.Fatalf("Push: %v", err)
	}

	var mu sync.Mutex
	calls := 0
	channel := channelCounterFunc(func() { mu.Lock(); calls++; mu.Unlock() })
	adapter := dbNudgeAdapter{store: store}

	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d := &DelegationDelivery{Channel: channel, Nudge: adapter, NudgeAfter: time.Minute}
			if _, err := d.NudgeUndrained(context.Background(), time.Now().Add(time.Hour), 10); err != nil {
				t.Errorf("NudgeUndrained: %v", err)
			}
		}()
	}
	wg.Wait()

	if calls != 1 {
		t.Fatalf("channel called %d times, want exactly 1 under concurrent sweep passes", calls)
	}
}
