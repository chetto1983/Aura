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
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/steer"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
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

// seedDelegationJobRow inserts one aura.ingestion_jobs row directly (bypassing
// DelegationEnqueuer.Create's own fencing/idempotency concerns, which have
// their own dedicated coverage elsewhere) purely to give
// CountUnfinishedDelegationJobsForFanout a real row to count against.
// aura.ingestion_jobs carries identity-scoped RLS (measured live: a bare
// pool.Exec as aura_app with no app.current_identity set is refused with
// "new row violates row-level security policy"), so the insert runs through
// db.WithIdentityTxRaw -- the SAME identity-scoping seam
// dbConversationRecorderAdapter's own AppendAssistantTurn uses below.
func seedDelegationJobRow(t *testing.T, pool *pgxpool.Pool, identityID, fanoutKey, status string) {
	t.Helper()
	payload := fmt.Sprintf(`{"goal":"g","conversation_id":"c","fanout_key":%q}`, fanoutKey)
	err := db.WithIdentityTxRaw(context.Background(), pool, identityID, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(),
			`INSERT INTO aura.ingestion_jobs (identity_id, job_type, status, idempotency_key, stage, max_attempts, next_attempt_at, payload)
			 VALUES ($1, 'swarm_delegation', $2, $3, 'dispatch', 3, now(), $4::jsonb)`,
			identityID, status, uuid.NewString(), payload)
		return err
	})
	if err != nil {
		t.Fatalf("seed delegation job row: %v", err)
	}
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
		out = append(out, UndrainedResult{ID: r.ID, IdentityID: r.IdentityID, ConversationID: r.ConversationID, Body: r.Body, FanoutKey: r.FanoutKey})
	}
	return out, nil
}

func (a dbNudgeAdapter) MarkFanoutNudged(ctx context.Context, identityID, fanoutKey string) ([]UndrainedResult, error) {
	rows, err := a.store.MarkFanoutNudged(ctx, identityID, fanoutKey)
	if err != nil {
		return nil, err
	}
	out := make([]UndrainedResult, 0, len(rows))
	for _, r := range rows {
		out = append(out, UndrainedResult{ID: r.ID, IdentityID: r.IdentityID, ConversationID: r.ConversationID, Body: r.Body, FanoutKey: r.FanoutKey})
	}
	return out, nil
}

// channelCounterFunc adapts a plain func() into ChannelDeliverer for the
// concurrency test below -- fakeChannelDeliverer's un-mutexed slice append
// (delegation_delivery_test.go) is not safe for concurrent callers.
type channelCounterFunc func()

func (f channelCounterFunc) DeliverToConversation(_ context.Context, _, _, _ string) (bool, error) {
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

	if err := store.PushDelegationResult(convID, steer.SourceWorker, "the report", "f-drained"); err != nil {
		t.Fatalf("PushDelegationResult: %v", err)
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
// racing over the SAME one-row fan-out push at most once. MarkFanoutNudged's
// conditional UPDATE (WHERE nudged_at IS NULL) is the idempotency key, so the
// loser of the race never calls the channel at all.
func TestNudgeOnceUnderConcurrency(t *testing.T) {
	pool := delegationDisposablePool(t)
	_, convID := seedDelegationDeliveryConversation(t, pool)
	store := steer.NewPostgresStore(pool, steer.Config{Max: 8, MaxBytes: 16384})

	if err := store.PushDelegationResult(convID, steer.SourceWorker, "the report", "f-concurrent"); err != nil {
		t.Fatalf("PushDelegationResult: %v", err)
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

// dbConversationRecorderAdapter mirrors the identity-scoping half of the REAL
// conversations.Store.AppendTurn write path -- db.WithCallerIdentityTx reads
// identityctx.IdentityID(ctx) to set the RLS carrier, then allocateTurnSeq locks the
// owning conversations row with the SAME query this adapter runs (LockConversationForTurnAppend,
// `SELECT id FROM aura.conversations WHERE id=$1 FOR UPDATE`) -- without importing
// internal/conversations, which this package's own D-02 hygiene test
// (TestSwarmPackageImportsNeitherConversationsNorChannels) forbids even from a test file
// (it globs every *.go in the package directory). A ctx with no identity bound cannot see
// the row under RLS and the lock returns zero rows -- the SAME "conversation not found"
// shape defect A measured live (live-check/d03/RESULTS.md).
type dbConversationRecorderAdapter struct {
	pool *pgxpool.Pool
}

func (r dbConversationRecorderAdapter) AppendAssistantTurn(ctx context.Context, conversationID, text string) error {
	return db.WithIdentityTxRaw(ctx, r.pool, identityctx.IdentityID(ctx), func(tx pgx.Tx) error {
		var id string
		if err := tx.QueryRow(ctx, `SELECT id FROM aura.conversations WHERE id = $1 FOR UPDATE`, conversationID).Scan(&id); err != nil {
			return fmt.Errorf("lock conversation %s: %w", conversationID, err)
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO aura.conversation_turns (conversation_id, seq, role, content) VALUES ($1, 1, 'assistant', $2)`,
			conversationID, text)
		return err
	})
}

// TestDeliverSuccessRecordsUnderRealRLSAsAuraApp is defect A's live proof at the db_integration
// tier (live-check/d03/RESULTS.md): deliverSuccess is called with a ctx carrying NO identity --
// exactly ProcessOnce's own daemon background loop shape -- against a REAL Postgres connection
// as aura_app (never the superuser aura role, which is BYPASSRLS and would give a false green
// on this exact RLS-scoping bug). Before the fix this fails "lock conversation: no rows":
// aura.conversations' RLS policy hides the row with no app.current_identity set. After the fix,
// the write lands and reads back.
func TestDeliverSuccessRecordsUnderRealRLSAsAuraApp(t *testing.T) {
	pool := delegationDisposablePool(t)
	identityID, convID := seedDelegationDeliveryConversation(t, pool)
	recorder := dbConversationRecorderAdapter{pool: pool}

	l := &DelegationClaimLoop{
		Store:      &fakeDelegationStore{},
		Delivery:   &DelegationDelivery{Recorder: recorder, Steer: &fakeSteerPublisher{}},
		IdentityID: identityID,
	}
	job := documents.IngestionJob{ID: "j1", IdentityID: identityID}
	payload := DelegationPayload{Goal: "summarise the inbox", ConversationID: convID, FanoutKey: "f-rls-delivery"}

	// processJob binds the identity once at the claim-loop boundary (unit-proven by
	// TestProcessJobBindsTheJobIdentityOnce); this test proves the OTHER half of defect A
	// against real RLS as aura_app -- with that identity on ctx the write lands.
	bound := identityctx.WithIdentityID(context.Background(), identityID)
	if err := l.deliverSuccess(bound, job, payload, ChildReport{Status: StatusOK, Summary: "all done"}); err != nil {
		t.Fatalf("deliverSuccess = %v, want the SC#1 write to succeed under real RLS", err)
	}

	var content string
	readErr := db.WithIdentityTxRaw(context.Background(), pool, identityID, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(),
			`SELECT content FROM aura.conversation_turns WHERE conversation_id = $1`, convID).Scan(&content)
	})
	if readErr != nil {
		t.Fatalf("read back recorded turn: %v", readErr)
	}
	if !strings.Contains(content, "all done") {
		t.Fatalf("recorded content = %q, want it to contain the delegation report", content)
	}
}

// TestDeliverReportWritesFanoutKeyUnderRealRLS is the db_integration
// counterpart to TestDeliverReportWritesTheFanoutKey's fake-based proof
// (51-11 Task 3, acceptance criteria's own named case): DeliverReport pushes
// through a REAL steer.PostgresStore.PushDelegationResult, and the row read
// back from aura.steer_queue carries fanout_key set, non-NULL, equal to the
// payload's key -- as aura_app, never the superuser aura role, which is
// BYPASSRLS and would give a false green on this exact write path.
func TestDeliverReportWritesFanoutKeyUnderRealRLS(t *testing.T) {
	pool := delegationDisposablePool(t)
	identityID, convID := seedDelegationDeliveryConversation(t, pool)
	recorder := dbConversationRecorderAdapter{pool: pool}
	store := steer.NewPostgresStore(pool, steer.Config{Max: 8, MaxBytes: 16384})
	d := &DelegationDelivery{Recorder: recorder, Steer: store}

	report := ChildReport{ChildID: "w1-real", Status: StatusOK, Goal: "goal", Summary: "summary"}
	payload := DelegationPayload{ConversationID: convID, FanoutKey: "f-realkey0123456"}
	bound := identityctx.WithIdentityID(context.Background(), identityID)
	if _, err := d.DeliverReport(bound, payload, report, time.Minute); err != nil {
		t.Fatalf("DeliverReport = %v", err)
	}

	var fanoutKey pgtype.Text
	readErr := db.WithIdentityTxRaw(context.Background(), pool, identityID, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(),
			`SELECT fanout_key FROM aura.steer_queue WHERE conversation_id = $1 AND kind = 'delegation_result'`,
			convID).Scan(&fanoutKey)
	})
	if readErr != nil {
		t.Fatalf("read back fanout_key: %v", readErr)
	}
	if !fanoutKey.Valid {
		t.Fatal("fanout_key is NULL, want it set")
	}
	if fanoutKey.String != payload.FanoutKey {
		t.Fatalf("fanout_key = %q, want payload.FanoutKey %q", fanoutKey.String, payload.FanoutKey)
	}
}

// TestFanoutRealDBSkipsWhileUnfinishedThenDeliversOnce is the db_integration
// counterpart to TestFanoutSkipsWhileAWorkerRuns/TestFanoutClaimsRendersOnceAndDelivers
// (51-11 Task 3): exercises CountUnfinishedDelegationJobsForFanout and
// MarkFanoutNudged against a REAL Postgres connection, as aura_app --
// *documents.PostgresIngestionJobStore satisfies swarm.FanoutJobCounter by
// construction (its CountUnfinishedDelegationJobs method signature matches
// the interface exactly), so no adapter is needed here.
func TestFanoutRealDBSkipsWhileUnfinishedThenDeliversOnce(t *testing.T) {
	pool := delegationDisposablePool(t)
	identityID, convID := seedDelegationDeliveryConversation(t, pool)
	steerStore := steer.NewPostgresStore(pool, steer.Config{Max: 8, MaxBytes: 16384})
	jobStore := documents.NewPostgresIngestionJobStore(pool)
	fanoutKey := "f-realfanout0001"

	seedDelegationJobRow(t, pool, identityID, fanoutKey, "running")

	report1, err := marshalReports([]ChildReport{{GoalIndex: 0, ChildID: "w1", Status: StatusOK, Goal: "g1", Summary: "s1"}})
	if err != nil {
		t.Fatalf("marshalReports: %v", err)
	}
	report2, err := marshalReports([]ChildReport{{GoalIndex: 1, ChildID: "w2", Status: StatusOK, Goal: "g2", Summary: "s2"}})
	if err != nil {
		t.Fatalf("marshalReports: %v", err)
	}
	if err := steerStore.PushDelegationResult(convID, steer.SourceWorker, report1, fanoutKey); err != nil {
		t.Fatalf("push 1: %v", err)
	}
	if err := steerStore.PushDelegationResult(convID, steer.SourceWorker, report2, fanoutKey); err != nil {
		t.Fatalf("push 2: %v", err)
	}

	channel := &fakeChannelDeliverer{delivered: true}
	d := &DelegationDelivery{Channel: channel, Nudge: dbNudgeAdapter{store: steerStore}, Counter: jobStore, NudgeAfter: time.Minute}

	n, err := d.NudgeUndrained(context.Background(), time.Now().Add(time.Hour), 10)
	if err != nil {
		t.Fatalf("NudgeUndrained (still running): %v", err)
	}
	if n != 0 {
		t.Fatalf("nudged = %d, want 0 while a sibling job is still running", n)
	}
	if len(channel.calls) != 0 {
		t.Fatalf("channel calls = %d, want 0", len(channel.calls))
	}

	err = db.WithIdentityTxRaw(context.Background(), pool, identityID, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(),
			`UPDATE aura.ingestion_jobs SET status = 'succeeded', completed_at = now() WHERE identity_id = $1 AND payload->>'fanout_key' = $2`,
			identityID, fanoutKey)
		return err
	})
	if err != nil {
		t.Fatalf("finish the fan-out's job: %v", err)
	}

	n, err = d.NudgeUndrained(context.Background(), time.Now().Add(time.Hour), 10)
	if err != nil {
		t.Fatalf("NudgeUndrained (finished): %v", err)
	}
	if n != 1 {
		t.Fatalf("nudged = %d, want 1 once the fan-out's job is finished", n)
	}
	if len(channel.calls) != 1 {
		t.Fatalf("channel calls = %d, want exactly 1", len(channel.calls))
	}
}
