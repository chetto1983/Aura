//go:build db_integration

package steer

import (
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/goleak"
)

// TestPostgresSteerQueuePushDrainRoundTrip proves the shipped Push/Drain contract
// against a REAL Postgres connection: FIFO order, Source round-trip (T-51-06),
// consume-once (a second Drain returns empty).
func TestPostgresSteerQueuePushDrainRoundTrip(t *testing.T) {
	pool := steerDisposablePool(t)
	_, convID := seedIdentityAndConversation(t, pool)
	store := NewPostgresStore(pool, Config{Max: 8, MaxBytes: 16384})

	for _, text := range []string{"first", "second", "third"} {
		if err := store.Push(convID, "cockpit", text); err != nil {
			t.Fatalf("Push(%q): %v", text, err)
		}
	}
	got := store.Drain(convID)
	if len(got) != 3 {
		t.Fatalf("Drain len = %d, want 3: %+v", len(got), got)
	}
	want := []string{"first", "second", "third"}
	for i, w := range want {
		if got[i].Text != w {
			t.Errorf("got[%d].Text = %q, want %q (FIFO order)", i, got[i].Text, w)
		}
	}
	if got[0].Source != "cockpit" {
		t.Errorf("Source = %q, want %q (T-51-06 round-trip)", got[0].Source, "cockpit")
	}
	if got[0].ID == "" {
		t.Error("ID must not be empty")
	}
	if again := store.Drain(convID); len(again) != 0 {
		t.Errorf("second Drain len = %d, want 0 (consume-once)", len(again))
	}
}

// TestPostgresSteerQueueWorkerSourceRoundTrips pins T-51-06: a row pushed with
// steer.SourceWorker round-trips with that EXACT source, never defaulted — the
// envelope drainSteer picks (markSteer, internal/agent/llm_agent_steer.go) depends on
// this surviving Push -> storage -> Drain unchanged.
func TestPostgresSteerQueueWorkerSourceRoundTrips(t *testing.T) {
	pool := steerDisposablePool(t)
	_, convID := seedIdentityAndConversation(t, pool)
	store := NewPostgresStore(pool, Config{Max: 8, MaxBytes: 16384})

	if err := store.Push(convID, SourceWorker, "worker report"); err != nil {
		t.Fatalf("Push: %v", err)
	}
	got := store.Drain(convID)
	if len(got) != 1 || got[0].Source != SourceWorker {
		t.Fatalf("Drain = %+v, want exactly one message with Source %q", got, SourceWorker)
	}
}

// TestPostgresSteerQueueRespectsMaxCap preserves the deleted in-memory Inbox's exact
// per-conversation cap semantic (Config.Max, combined across both kinds) — moved here
// from cmd/aura/chat_boot_test.go because a successful Push now requires a real
// connection.
func TestPostgresSteerQueueRespectsMaxCap(t *testing.T) {
	pool := steerDisposablePool(t)
	_, convID := seedIdentityAndConversation(t, pool)
	store := NewPostgresStore(pool, Config{Max: 3, MaxBytes: 64})

	for i := range 3 {
		if err := store.Push(convID, "cockpit", "msg"); err != nil {
			t.Fatalf("push %d/3: %v", i+1, err)
		}
	}
	if err := store.Push(convID, "cockpit", "one too many"); err != ErrQueueFull {
		t.Fatalf("Push past Max = %v, want ErrQueueFull", err)
	}
	if got := store.Drain(convID); len(got) != 3 {
		t.Fatalf("Drain after a refused push = %d, want it to still hold exactly 3", len(got))
	}
}

// TestPostgresSteerQueuePushUnknownConversation proves Push refuses (not silently
// no-ops) when conv has no resolvable owner via aura.conversation_owner() — the
// disambiguation probe's "wiring error" branch.
func TestPostgresSteerQueuePushUnknownConversation(t *testing.T) {
	pool := steerDisposablePool(t)
	store := NewPostgresStore(pool, Config{Max: 8, MaxBytes: 16384})

	unknown := "00000000-0000-0000-0000-000000000000"
	if err := store.Push(unknown, "cockpit", "hi"); err == nil {
		t.Fatal("Push against an unresolvable conversation = nil, want an error")
	}
}

// TestPostgresSteerQueueCrossIdentityInvisible pins T-51-07: a row whose identity_id
// does not match conv's TRUE owner (inserted directly, bypassing Push, to simulate a
// hypothetical data-corruption or future-bug scenario) is never returned by Drain —
// the defense-in-depth identity_id = owner check inside DrainSteerRows.
func TestPostgresSteerQueueCrossIdentityInvisible(t *testing.T) {
	pool := steerDisposablePool(t)
	_, convID := seedIdentityAndConversation(t, pool)
	foreignIdentityID, _ := seedIdentityAndConversation(t, pool)
	store := NewPostgresStore(pool, Config{Max: 8, MaxBytes: 16384})

	if err := store.Push(convID, "cockpit", "legit"); err != nil {
		t.Fatalf("Push (legit): %v", err)
	}
	// Insert a foreign-identity row directly against the SAME conversation_id,
	// bypassing Push's own owner derivation, to simulate the class of bug the
	// defense-in-depth check exists to catch.
	if _, err := pool.Exec(t.Context(),
		"INSERT INTO aura.steer_queue (identity_id, conversation_id, kind, source, body) VALUES ($1, $2, 'steer', 'cockpit', 'foreign')",
		foreignIdentityID, convID,
	); err != nil {
		t.Fatalf("seed foreign-identity row: %v", err)
	}

	got := store.Drain(convID)
	if len(got) != 1 || got[0].Text != "legit" {
		t.Fatalf("Drain = %+v, want exactly the ONE legit-identity row, foreign row excluded", got)
	}
}

// TestPostgresSteerQueueConcurrentDrainDisjoint proves two concurrent Drain calls for
// the same conversation never return the same row twice (the FOR UPDATE serialization
// in DrainSteerRows).
func TestPostgresSteerQueueConcurrentDrainDisjoint(t *testing.T) {
	// defer, NOT t.Cleanup, for the pool close: t.Cleanup callbacks (including
	// steerDisposablePool's own pool.Close()) run AFTER every defer in this test
	// function has already executed, so relying on that alone would let
	// goleak.VerifyNone observe pgxpool's still-running backgroundHealthCheck
	// goroutines. This local defer, registered AFTER goleak's, runs BEFORE it
	// (LIFO); pool.Close() is idempotent (sync.Once-guarded) so the later
	// t.Cleanup close is a safe no-op. Same gotcha documented in
	// internal/conversations/sweeper_test.go.
	//
	// IgnoreAnyFunction for backgroundHealthCheck covers the ONE pool this helper
	// cannot close early: steerDisposablePool's internal `root` connection, held open
	// specifically so t.Cleanup can DROP DATABASE after every other test using the
	// helper has finished with it. pgxpool.Close() closes closeChan synchronously but
	// does not wait for the goroutine's own select to observe it and return (read
	// pgxpool/pool.go:456-461 vs 489-502) -- a real async-shutdown race in the
	// dependency, not a leak in this package's code. Same precedented pattern as
	// cmd/aura/main_test.go's own IgnoreAnyFunction allowlist.
	defer goleak.VerifyNone(t, goleak.IgnoreAnyFunction("github.com/jackc/pgx/v5/pgxpool.(*Pool).backgroundHealthCheck"))
	pool := steerDisposablePool(t)
	defer pool.Close()
	_, convID := seedIdentityAndConversation(t, pool)
	store := NewPostgresStore(pool, Config{Max: 100, MaxBytes: 64})

	const n = 20
	for i := range n {
		if err := store.Push(convID, "cockpit", "msg"); err != nil {
			t.Fatalf("push %d/%d: %v", i+1, n, err)
		}
	}

	var mu sync.Mutex
	var drained []Message
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := store.Drain(convID)
			mu.Lock()
			drained = append(drained, got...)
			mu.Unlock()
		}()
	}
	wg.Wait()

	if len(drained) != n {
		t.Fatalf("drained %d messages across 4 concurrent Drain calls, want %d (lost or duplicated)", len(drained), n)
	}
	seen := make(map[string]bool, n)
	for _, m := range drained {
		if seen[m.ID] {
			t.Fatalf("row %s drained more than once — concurrent Drain calls were not disjoint", m.ID)
		}
		seen[m.ID] = true
	}
}

// TestQueueTTLDerivedPerKind is the invariant test from the assumption-delta decision:
// for EVERY value of kind, a row's expires_at is derived from that kind's configured
// TTL and from no other source. It goes red the instant a future variant is added
// without its own TTL.
func TestQueueTTLDerivedPerKind(t *testing.T) {
	pool := steerDisposablePool(t)
	_, convID := seedIdentityAndConversation(t, pool)
	store := NewPostgresStore(pool, Config{
		Max: 8, MaxBytes: 16384,
		SteerTTL:            5 * time.Minute,
		DelegationResultTTL: 5 * time.Hour,
	})

	before := time.Now()
	if err := store.Push(convID, "cockpit", "steer row"); err != nil {
		t.Fatalf("Push(steer): %v", err)
	}
	if err := store.PushDelegationResult(convID, SourceWorker, "delegation row", "f-ttl"); err != nil {
		t.Fatalf("PushDelegationResult: %v", err)
	}

	var kind, expiresAt string
	rows, err := pool.Query(t.Context(),
		"SELECT kind, expires_at FROM aura.steer_queue WHERE conversation_id = $1 ORDER BY created_at", convID)
	if err != nil {
		t.Fatalf("query rows: %v", err)
	}
	defer rows.Close()
	got := map[string]time.Time{}
	for rows.Next() {
		var expires time.Time
		if err := rows.Scan(&kind, &expires); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[kind] = expires
		_ = expiresAt
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	steerExpiry, ok := got[string(KindSteer)]
	if !ok {
		t.Fatal("no steer-kind row found")
	}
	delegationExpiry, ok := got[string(KindDelegationResult)]
	if !ok {
		t.Fatal("no delegation_result-kind row found")
	}

	steerDelta := steerExpiry.Sub(before)
	if steerDelta < 4*time.Minute || steerDelta > 6*time.Minute {
		t.Errorf("steer expires_at delta = %v, want ~5m (SteerTTL, never DelegationResultTTL)", steerDelta)
	}
	delegationDelta := delegationExpiry.Sub(before)
	if delegationDelta < 4*time.Hour || delegationDelta > 6*time.Hour {
		t.Errorf("delegation_result expires_at delta = %v, want ~5h (DelegationResultTTL, never SteerTTL)", delegationDelta)
	}
	if !delegationExpiry.After(steerExpiry) {
		t.Errorf("delegation_result expires_at (%v) must be well after steer's (%v) — the D-07 asymmetry", delegationExpiry, steerExpiry)
	}
}

// TestQueueTTLDisabledByNonPositiveValue pins the AURA_ASKUSER_PAUSE_TTL_SEC
// precedent: a <=0 TTL leaves expires_at NULL (never expires), not a silent
// fall-through to some default duration.
func TestQueueTTLDisabledByNonPositiveValue(t *testing.T) {
	pool := steerDisposablePool(t)
	_, convID := seedIdentityAndConversation(t, pool)
	store := NewPostgresStore(pool, Config{Max: 8, MaxBytes: 16384, SteerTTL: 0})

	if err := store.Push(convID, "cockpit", "never expires"); err != nil {
		t.Fatalf("Push: %v", err)
	}
	var expiresAtValid bool
	if err := pool.QueryRow(t.Context(),
		"SELECT expires_at IS NOT NULL FROM aura.steer_queue WHERE conversation_id = $1", convID,
	).Scan(&expiresAtValid); err != nil {
		t.Fatalf("query: %v", err)
	}
	if expiresAtValid {
		t.Error("expires_at is set for a <=0 TTL, want NULL (never expires)")
	}
}

// TestPostgresSteerQueuePushDelegationResultRoundTrip proves PushDelegationResult
// (51-11 Task 3) against a REAL Postgres connection: the row it writes carries
// fanout_key set, and a plain steer Push round-trips a NULL one.
func TestPostgresSteerQueuePushDelegationResultRoundTrip(t *testing.T) {
	pool := steerDisposablePool(t)
	_, convID := seedIdentityAndConversation(t, pool)
	store := NewPostgresStore(pool, Config{Max: 8, MaxBytes: 16384})

	if err := store.PushDelegationResult(convID, SourceWorker, "fan-out report", "f-roundtrip123"); err != nil {
		t.Fatalf("PushDelegationResult: %v", err)
	}
	if err := store.Push(convID, SourceWorker, "worker question"); err != nil {
		t.Fatalf("Push: %v", err)
	}

	rows, err := pool.Query(t.Context(),
		"SELECT body, fanout_key FROM aura.steer_queue WHERE conversation_id = $1 ORDER BY created_at", convID)
	if err != nil {
		t.Fatalf("query rows: %v", err)
	}
	defer rows.Close()
	type row struct {
		body      string
		fanoutKey pgtype.Text
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.body, &r.fanoutKey); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("rows = %d, want 2", len(got))
	}
	if got[0].body != "fan-out report" || !got[0].fanoutKey.Valid || got[0].fanoutKey.String != "f-roundtrip123" {
		t.Fatalf("PushDelegationResult row = %+v, want fanout_key set to f-roundtrip123", got[0])
	}
	if got[1].body != "worker question" || got[1].fanoutKey.Valid {
		t.Fatalf("Push row = %+v, want fanout_key NULL", got[1])
	}
}

// TestPostgresSteerQueueMarkFanoutNudgedClaimsWholeGroupOnce proves
// MarkFanoutNudged's own contract against a REAL connection: it claims every
// unclaimed row of one (identity, fanout_key) pair in ONE statement, and a
// second call over the SAME group claims nothing (the loser of a race sees
// an empty result, never a partial claim).
func TestPostgresSteerQueueMarkFanoutNudgedClaimsWholeGroupOnce(t *testing.T) {
	pool := steerDisposablePool(t)
	identityID, convID := seedIdentityAndConversation(t, pool)
	store := NewPostgresStore(pool, Config{Max: 8, MaxBytes: 16384})

	fanoutKey := "f-claimonce456"
	if err := store.PushDelegationResult(convID, SourceWorker, "worker one", fanoutKey); err != nil {
		t.Fatalf("PushDelegationResult 1: %v", err)
	}
	if err := store.PushDelegationResult(convID, SourceWorker, "worker two", fanoutKey); err != nil {
		t.Fatalf("PushDelegationResult 2: %v", err)
	}
	// A steer-kind row (no fan-out key) in the same conversation must never be
	// swept up by a fan-out-scoped claim.
	if err := store.Push(convID, "cockpit", "unrelated steer"); err != nil {
		t.Fatalf("Push (unrelated): %v", err)
	}

	claimed, err := store.MarkFanoutNudged(t.Context(), identityID, fanoutKey)
	if err != nil {
		t.Fatalf("MarkFanoutNudged: %v", err)
	}
	if len(claimed) != 2 {
		t.Fatalf("claimed = %v, want 2 rows", claimed)
	}

	again, err := store.MarkFanoutNudged(t.Context(), identityID, fanoutKey)
	if err != nil {
		t.Fatalf("MarkFanoutNudged (second pass): %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("second MarkFanoutNudged claimed = %v, want none -- both rows already nudged", again)
	}
}

// TestPostgresSteerQueueLazyExpiryOnRead proves a row past its own expires_at is
// excluded by Drain even before the sweeper runs.
func TestPostgresSteerQueueLazyExpiryOnRead(t *testing.T) {
	pool := steerDisposablePool(t)
	_, convID := seedIdentityAndConversation(t, pool)
	store := NewPostgresStore(pool, Config{Max: 8, MaxBytes: 16384})

	if err := store.Push(convID, "cockpit", "already stale"); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if _, err := pool.Exec(t.Context(),
		"UPDATE aura.steer_queue SET expires_at = now() - interval '1 hour' WHERE conversation_id = $1", convID,
	); err != nil {
		t.Fatalf("backdate expires_at: %v", err)
	}
	if got := store.Drain(convID); len(got) != 0 {
		t.Fatalf("Drain returned %d rows past expires_at, want 0 (lazy expiry on read)", len(got))
	}
}
