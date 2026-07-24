//go:build db_integration

package adaptive

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func adaptiveIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	required := func(key string) string {
		t.Helper()
		value := os.Getenv(key)
		if value == "" {
			if os.Getenv("CI") != "" {
				t.Fatalf("integration test requires %s under CI", key)
			}
			t.Skipf("integration test requires %s", key)
		}
		return value
	}
	password := required("POSTGRES_PASSWORD")
	migrateURL := required("AURA_DB_MIGRATE_URL")
	appURL := required("AURA_DB_URL")
	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	bootstrap := fmt.Sprintf("postgres://aura:%s@%s:%s/aura?sslmode=disable", password, host, port)
	if err := db.EnsureRoles(ctx, bootstrap, password); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	if _, err := db.Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := db.Open(ctx, &db.Config{URL: appURL})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestStoreRecordOrdersAndRejectsPayloadConflict(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx := context.Background()
	owner := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')`,
		owner, "adaptive-proof-"+owner.String()); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.adaptive_outbox WHERE owner_id=$1`, owner)
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.adaptive_aggregate_sequences WHERE owner_id=$1`, owner)
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id=$1`, owner)
	})

	store := NewStore(pool, StoreConfig{MaxAttempts: 3})
	decision := uuid.Must(uuid.NewV7())
	first, err := NewEvent(EventParams{
		ID: uuid.Must(uuid.NewV7()), OwnerID: owner, AggregateID: "request-1",
		DecisionID: decision, Kind: EventDecision,
		Payload: []byte(`{"schema_version":"1.0","action":"off"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewEvent(EventParams{
		ID: uuid.Must(uuid.NewV7()), OwnerID: owner, AggregateID: "request-1",
		DecisionID: decision, Kind: EventOutcome,
		Payload: []byte(`{"schema_version":"1.0","quality":1}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	seq1, err := store.Record(ctx, first)
	if err != nil {
		t.Fatalf("Record(first): %v", err)
	}
	seq2, err := store.Record(ctx, second)
	if err != nil {
		t.Fatalf("Record(second): %v", err)
	}
	if seq1 != 1 || seq2 != 2 {
		t.Fatalf("sequences = %d,%d, want 1,2", seq1, seq2)
	}
	retrySeq, err := store.Record(ctx, first)
	if err != nil || retrySeq != seq1 {
		t.Fatalf("idempotent Record(first) = (%d,%v), want (%d,nil)", retrySeq, err, seq1)
	}

	conflict, err := NewEvent(EventParams{
		ID: first.ID, OwnerID: owner, AggregateID: "request-1",
		DecisionID: decision, Kind: EventDecision,
		Payload: []byte(`{"schema_version":"1.0","action":"high"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Record(ctx, conflict); !errors.Is(err, ErrPayloadConflict) {
		t.Fatalf("conflicting duplicate error = %v, want ErrPayloadConflict", err)
	}

	rows, err := store.ListAggregate(ctx, owner, "request-1")
	if err != nil {
		t.Fatalf("ListAggregate: %v", err)
	}
	if len(rows) != 2 || rows[0].Sequence != 1 || rows[1].Sequence != 2 {
		t.Fatalf("aggregate rows = %+v, want exactly ordered sequences 1,2", rows)
	}
}

func TestAdaptiveRuntimeCannotMutateFactsDeleteRowsOrBypassSequenceAllocator(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx := context.Background()
	owner := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1,$2,'user')`,
		owner, "adaptive-immutable-"+owner.String()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id=$1`, owner)
	})
	store := NewStore(pool, StoreConfig{MaxAttempts: 3})
	event, err := NewEvent(EventParams{
		ID: uuid.Must(uuid.NewV7()), OwnerID: owner, AggregateID: "immutable-ledger",
		DecisionID: uuid.Must(uuid.NewV7()), Kind: EventDecision,
		Payload: []byte(`{"schema_version":"1.0","action":"static"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Record(ctx, event); err != nil {
		t.Fatalf("Record: %v", err)
	}

	for name, statement := range map[string]string{
		"payload": `UPDATE aura.adaptive_outbox
			SET payload='{"schema_version":"1.0","action":"tampered"}'::jsonb WHERE id=$1`,
		"owner":  `UPDATE aura.adaptive_outbox SET owner_id=gen_random_uuid() WHERE id=$1`,
		"hash":   `UPDATE aura.adaptive_outbox SET payload_hash=decode(repeat('00',32),'hex') WHERE id=$1`,
		"delete": `DELETE FROM aura.adaptive_outbox WHERE id=$1`,
	} {
		if _, err := pool.Exec(ctx, statement, event.ID); err == nil {
			t.Fatalf("runtime raw %s mutation succeeded", name)
		}
	}
	for name, statement := range map[string]string{
		"update": `UPDATE aura.adaptive_aggregate_sequences
			SET next_sequence=next_sequence+100 WHERE owner_id=$1 AND aggregate_id='immutable-ledger'`,
		"delete": `DELETE FROM aura.adaptive_aggregate_sequences
			WHERE owner_id=$1 AND aggregate_id='immutable-ledger'`,
	} {
		if _, err := pool.Exec(ctx, statement, owner); err == nil {
			t.Fatalf("runtime raw sequence %s succeeded", name)
		}
	}

	now := time.Now().UTC()
	claimed, err := store.Claim(ctx, ClaimOptions{
		WorkerID: "immutable-worker-1", Now: now, Lease: time.Minute,
	})
	if err != nil || claimed == nil || claimed.ID != event.ID {
		t.Fatalf("Claim = (%+v,%v), want immutable event", claimed, err)
	}
	if err := store.Retry(ctx, event.ID, "immutable-worker-1", "transient",
		now, now.Add(time.Nanosecond)); err != nil {
		t.Fatalf("Retry through operational columns: %v", err)
	}
	reclaimed, err := store.Claim(ctx, ClaimOptions{
		WorkerID: "immutable-worker-2", Now: now.Add(time.Second), Lease: time.Minute,
	})
	if err != nil || reclaimed == nil || reclaimed.ID != event.ID {
		t.Fatalf("reclaim = (%+v,%v), want immutable event", reclaimed, err)
	}
	if err := store.MarkProjected(ctx, event.ID, "immutable-worker-2", now.Add(time.Second)); err != nil {
		t.Fatalf("MarkProjected through operational columns: %v", err)
	}

	nextEvent, err := NewEvent(EventParams{
		ID: uuid.Must(uuid.NewV7()), OwnerID: owner, AggregateID: "immutable-ledger",
		DecisionID: uuid.Must(uuid.NewV7()), Kind: EventOutcome,
		Payload: []byte(`{"schema_version":"1.0","quality_observed":false}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if sequence, err := store.Record(ctx, nextEvent); err != nil || sequence != 2 {
		t.Fatalf("second Record sequence = %d err %v, want 2/nil", sequence, err)
	}
}

func TestAdaptiveOutboxRLSHidesAndRejectsCrossOwnerRows(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx := context.Background()
	ownerA := uuid.Must(uuid.NewV7())
	ownerB := uuid.Must(uuid.NewV7())
	for _, owner := range []uuid.UUID{ownerA, ownerB} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')`,
			owner, "adaptive-rls-"+owner.String()); err != nil {
			t.Fatalf("seed owner %s: %v", owner, err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id = ANY($1::uuid[])`,
			[]uuid.UUID{ownerA, ownerB})
	})

	store := NewStore(pool, StoreConfig{})
	for _, owner := range []uuid.UUID{ownerA, ownerB} {
		event, err := NewEvent(EventParams{
			ID: uuid.Must(uuid.NewV7()), OwnerID: owner, AggregateID: "rls-proof",
			DecisionID: uuid.Must(uuid.NewV7()), Kind: EventDecision,
			Payload: []byte(`{"schema_version":"1.0","domain":"tool"}`),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Record(ctx, event); err != nil {
			t.Fatalf("Record(%s): %v", owner, err)
		}
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.current_identity', $1, true)`, ownerA.String()); err != nil {
		t.Fatalf("set owner scope: %v", err)
	}
	var visible int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM aura.adaptive_outbox`).Scan(&visible); err != nil {
		t.Fatalf("count owner-scoped outbox: %v", err)
	}
	if visible != 1 {
		t.Fatalf("owner A sees %d adaptive rows, want only its own row", visible)
	}

	foreignEvent := uuid.Must(uuid.NewV7())
	_, err = tx.Exec(ctx, `
INSERT INTO aura.adaptive_outbox (
    id, owner_id, aggregate_id, sequence, decision_id, event_kind,
    payload, payload_hash, status, attempts, available_at, created_at
) VALUES ($1, $2, 'cross-owner-insert', 1, $3, 'decision',
          '{"schema_version":"1.0"}'::jsonb, $4, 'pending', 0, now(), now())`,
		foreignEvent, ownerB, uuid.Must(uuid.NewV7()), make([]byte, 32))
	if err == nil {
		t.Fatal("owner A inserted an owner B adaptive row; want RLS rejection")
	}
}

func TestStoreRejectsRecreatedDeletedOwner(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx := context.Background()
	owner := uuid.Must(uuid.NewV7())
	name := "adaptive-tombstone-" + owner.String()
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')`,
		owner, name); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id=$1`, owner)
	})

	store := NewStore(pool, StoreConfig{})
	decision := uuid.Must(uuid.NewV7())
	event, err := NewEvent(EventParams{
		ID: uuid.Must(uuid.NewV7()), OwnerID: owner, AggregateID: "request-before-delete",
		DecisionID: decision, Kind: EventDecision,
		Payload: []byte(`{"schema_version":"1.0","action":"off"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Record(ctx, event); err != nil {
		t.Fatalf("Record(before delete): %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM aura.identities WHERE id=$1`, owner); err != nil {
		t.Fatalf("delete owner: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`DELETE FROM aura.adaptive_identity_tombstones WHERE owner_id=$1`, owner); err == nil {
		t.Fatal("runtime role deleted a permanent adaptive identity tombstone")
	}
	if _, err := pool.Exec(ctx,
		`UPDATE aura.adaptive_identity_tombstones SET owner_id=gen_random_uuid() WHERE owner_id=$1`,
		owner); err == nil {
		t.Fatal("runtime role moved a permanent adaptive identity tombstone")
	}
	var retainedNameColumns int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM information_schema.columns
WHERE table_schema='aura'
  AND table_name='adaptive_identity_tombstones'
  AND column_name='identity_name'`).Scan(&retainedNameColumns); err != nil {
		t.Fatal(err)
	}
	if retainedNameColumns != 0 {
		t.Fatal("permanent tombstone still retains identity_name")
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')`,
		owner, name+"-recreated"); err != nil {
		t.Fatalf("recreate owner UUID: %v", err)
	}
	after, err := NewEvent(EventParams{
		ID: uuid.Must(uuid.NewV7()), OwnerID: owner, AggregateID: "request-after-delete",
		DecisionID: uuid.Must(uuid.NewV7()), Kind: EventDecision,
		Payload: []byte(`{"schema_version":"1.0","action":"high"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Record(ctx, after); !errors.Is(err, ErrOwnerTombstoned) {
		t.Fatalf("Record(after owner recreation) error = %v, want ErrOwnerTombstoned", err)
	}
}

func TestStoreDeletionFenceClosesRecordWaitAndUUIDRecreationRace(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx := context.Background()
	owner := uuid.Must(uuid.NewV7())
	name := "adaptive-record-race-" + owner.String()
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1,$2,'user')`,
		owner, name); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id=$1`, owner)
	})

	event, err := NewEvent(EventParams{
		ID: uuid.Must(uuid.NewV7()), OwnerID: owner, AggregateID: "record-delete-race",
		DecisionID: uuid.Must(uuid.NewV7()), Kind: EventDecision,
		Payload: []byte(`{"schema_version":"1.0","action":"shadow"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	lockTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lockTx.Rollback(context.Background()) }()
	if _, err := lockTx.Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`,
		event.ID.String()); err != nil {
		t.Fatalf("hold event advisory lock: %v", err)
	}

	recordDone := make(chan error, 1)
	go func() {
		_, recordErr := NewStore(pool, StoreConfig{}).Record(ctx, event)
		recordDone <- recordErr
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		if err := pool.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM pg_stat_activity
    WHERE usename = current_user
      AND wait_event = 'advisory'
      AND query LIKE '%pg_advisory_xact_lock%'
)`).Scan(&waiting); err != nil {
			t.Fatalf("observe blocked Record: %v", err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Record did not reach the held event advisory lock")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM aura.identities WHERE id=$1`, owner); err != nil {
		t.Fatalf("delete owner during blocked Record: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1,$2,'user')`,
		owner, name+"-recreated"); err != nil {
		t.Fatalf("recreate owner during blocked Record: %v", err)
	}
	if err := lockTx.Commit(ctx); err != nil {
		t.Fatalf("release event advisory lock: %v", err)
	}
	if err := <-recordDone; !errors.Is(err, ErrOwnerTombstoned) {
		t.Fatalf("Record after delete+recreate race error = %v, want ErrOwnerTombstoned", err)
	}
	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM aura.adaptive_outbox WHERE id=$1`, event.ID,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("delete+recreate race inserted %d adaptive events, want 0", count)
	}
}

func TestStoreClaimsAcrossWorkersPreserveAggregateOrderAndLeaseOwnership(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx := context.Background()
	owner := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')`,
		owner, "adaptive-claims-"+owner.String()); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id=$1`, owner)
	})

	store := NewStore(pool, StoreConfig{MaxAttempts: 3})
	base := time.Now().UTC().Add(-time.Minute)
	makeEvent := func(aggregate string, offset time.Duration) Event {
		t.Helper()
		event, err := NewEvent(EventParams{
			ID: uuid.Must(uuid.NewV7()), OwnerID: owner, AggregateID: aggregate,
			DecisionID: uuid.Must(uuid.NewV7()), Kind: EventOutcome,
			Payload:   []byte(`{"schema_version":"1.0","quality":1}`),
			CreatedAt: base.Add(offset),
		})
		if err != nil {
			t.Fatal(err)
		}
		return event
	}
	a1 := makeEvent("aggregate-a", 0)
	a2 := makeEvent("aggregate-a", time.Second)
	b1 := makeEvent("aggregate-b", 2*time.Second)
	for _, event := range []Event{a1, a2, b1} {
		if _, err := store.Record(ctx, event); err != nil {
			t.Fatalf("Record(%s): %v", event.AggregateID, err)
		}
	}

	now := time.Now().UTC()
	first, err := store.Claim(ctx, ClaimOptions{WorkerID: "worker-1", Now: now, Lease: time.Minute})
	if err != nil {
		t.Fatalf("Claim(worker-1): %v", err)
	}
	if first == nil || first.ID != a1.ID {
		t.Fatalf("first claim = %+v, want aggregate-a sequence 1", first)
	}
	second, err := store.Claim(ctx, ClaimOptions{WorkerID: "worker-2", Now: now, Lease: time.Minute})
	if err != nil {
		t.Fatalf("Claim(worker-2): %v", err)
	}
	if second == nil || second.ID != b1.ID {
		t.Fatalf("second claim = %+v, want aggregate-b sequence 1 (not aggregate-a sequence 2)", second)
	}
	if err := store.MarkProjected(ctx, first.ID, "worker-1", now); err != nil {
		t.Fatalf("MarkProjected(worker-1): %v", err)
	}
	third, err := store.Claim(ctx, ClaimOptions{WorkerID: "worker-3", Now: now, Lease: time.Minute})
	if err != nil {
		t.Fatalf("Claim(worker-3): %v", err)
	}
	if third == nil || third.ID != a2.ID {
		t.Fatalf("third claim = %+v, want aggregate-a sequence 2 after sequence 1 projected", third)
	}
	if err := store.MarkProjected(ctx, second.ID, "stale-worker", now); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale MarkProjected error = %v, want ErrLeaseLost", err)
	}
	if err := store.MarkProjected(ctx, third.ID, "worker-3", now); err != nil {
		t.Fatalf("MarkProjected(worker-3): %v", err)
	}

	reclaimed, err := store.Claim(ctx, ClaimOptions{
		WorkerID: "worker-4",
		Now:      now.Add(2 * time.Minute),
		Lease:    time.Minute,
	})
	if err != nil {
		t.Fatalf("Claim(expired lease): %v", err)
	}
	if reclaimed == nil || reclaimed.ID != b1.ID || reclaimed.Attempts != 2 {
		t.Fatalf("reclaimed = %+v, want aggregate-b attempt 2", reclaimed)
	}
}

func TestStoreRetryTransitionsPendingThenDeadLetterWithTypedTimestamp(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx := context.Background()
	owner := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')`,
		owner, "adaptive-retry-"+owner.String()); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id=$1`, owner)
	})

	store := NewStore(pool, StoreConfig{MaxAttempts: 2})
	event, err := NewEvent(EventParams{
		ID: uuid.Must(uuid.NewV7()), OwnerID: owner, AggregateID: "retry-dead-letter",
		DecisionID: uuid.Must(uuid.NewV7()), Kind: EventOutcome,
		Payload: []byte(`{"schema_version":"1.0","quality_observed":false}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Record(ctx, event); err != nil {
		t.Fatalf("Record: %v", err)
	}

	firstAt := time.Now().UTC()
	first, err := store.Claim(ctx, ClaimOptions{WorkerID: "retry-worker-1", Now: firstAt, Lease: time.Minute})
	if err != nil {
		t.Fatalf("Claim(first): %v", err)
	}
	if first == nil || first.ID != event.ID || first.Attempts != 1 {
		t.Fatalf("first claim = %+v, want event attempt 1", first)
	}
	if err := store.Retry(ctx, event.ID, "retry-worker-1", "neo4j_unavailable",
		firstAt, firstAt.Add(time.Second)); err != nil {
		t.Fatalf("Retry(first): %v", err)
	}

	var status string
	var deadLetterAt *time.Time
	if err := pool.QueryRow(ctx,
		`SELECT status, dead_letter_at FROM aura.adaptive_outbox WHERE id=$1`,
		event.ID).Scan(&status, &deadLetterAt); err != nil {
		t.Fatalf("read first retry state: %v", err)
	}
	if status != "pending" || deadLetterAt != nil {
		t.Fatalf("first retry state = status %q dead_letter_at %v, want pending/nil",
			status, deadLetterAt)
	}

	secondAt := firstAt.Add(2 * time.Second)
	second, err := store.Claim(ctx, ClaimOptions{WorkerID: "retry-worker-2", Now: secondAt, Lease: time.Minute})
	if err != nil {
		t.Fatalf("Claim(second): %v", err)
	}
	if second == nil || second.ID != event.ID || second.Attempts != 2 {
		t.Fatalf("second claim = %+v, want event attempt 2", second)
	}
	if err := store.Retry(ctx, event.ID, "retry-worker-2", "neo4j_unavailable",
		secondAt, secondAt.Add(time.Second)); err != nil {
		t.Fatalf("Retry(second): %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT status, dead_letter_at FROM aura.adaptive_outbox WHERE id=$1`,
		event.ID).Scan(&status, &deadLetterAt); err != nil {
		t.Fatalf("read dead-letter state: %v", err)
	}
	timestampMatches := deadLetterAt != nil &&
		deadLetterAt.Sub(secondAt) > -time.Microsecond &&
		deadLetterAt.Sub(secondAt) < time.Microsecond
	if status != "dead_letter" || !timestampMatches {
		t.Fatalf("second retry state = status %q dead_letter_at %v, want dead_letter/%v",
			status, deadLetterAt, secondAt)
	}
}
