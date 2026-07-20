//go:build db_integration

package conversations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRolloutStoreRestartAndStaleDecision(t *testing.T) {
	pool, dsn, _ := compactionDB(t)
	scope := "deployment-" + uuid.NewString()
	store := NewCompactionRolloutStore(pool)
	initial := rolloutFixture(scope, 1, "disabled")
	if _, err := store.Create(t.Context(), initial); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Transition(t.Context(), transitionFixture(initial, "shadow")); err != nil {
		t.Fatal(err)
	}

	pool.Close()
	reopened, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	restarted := NewCompactionRolloutStore(reopened)
	got, err := restarted.Load(t.Context(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 2 || got.Stage != "shadow" {
		t.Fatalf("restart state=%+v", got)
	}
	_, err = restarted.Transition(t.Context(), transitionFixture(initial, "canary_1"))
	if !errors.Is(err, ErrRolloutStaleVersion) {
		t.Fatalf("stale error=%v", err)
	}
}

func TestRolloutStoreMultiReplicaExactlyOneWins(t *testing.T) {
	pool, dsn, _ := compactionDB(t)
	scope := "deployment-" + uuid.NewString()
	initial := rolloutFixture(scope, 1, "disabled")
	if _, err := NewCompactionRolloutStore(pool).Create(t.Context(), initial); err != nil {
		t.Fatal(err)
	}
	replica, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(replica.Close)
	stores := []*CompactionRolloutStore{NewCompactionRolloutStore(pool), NewCompactionRolloutStore(replica)}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i, stage := range []string{"shadow", "canary_1"} {
		wg.Add(1)
		go func(store *CompactionRolloutStore, next string) {
			defer wg.Done()
			_, err := store.Transition(context.Background(), transitionFixture(initial, next))
			errs <- err
		}(stores[i], stage)
	}
	wg.Wait()
	close(errs)
	wins, stale := 0, 0
	for err := range errs {
		if err == nil {
			wins++
		} else if errors.Is(err, ErrRolloutStaleVersion) {
			stale++
		} else {
			t.Fatal(err)
		}
	}
	if wins != 1 || stale != 1 {
		t.Fatalf("wins=%d stale=%d", wins, stale)
	}
}

func TestRolloutStoreAtomicRollbackRestoresLastKnownGood(t *testing.T) {
	pool, _, _ := compactionDB(t)
	scope := "deployment-" + uuid.NewString()
	store := NewCompactionRolloutStore(pool)
	initial := rolloutFixture(scope, 1, "disabled")
	if _, err := store.Create(t.Context(), initial); err != nil {
		t.Fatal(err)
	}
	active, err := store.Transition(t.Context(), transitionFixture(initial, "canary_5"))
	if err != nil {
		t.Fatal(err)
	}
	rolled, err := store.Rollback(t.Context(), RolloutRollback{ScopeID: scope, ExpectedVersion: active.Version, Evidence: evidenceFixture(scope, "rollback"), ReasonCode: "restore_rate_exceeded"})
	if err != nil {
		t.Fatal(err)
	}
	if rolled.Version != 3 || rolled.Stage != "disabled" || !jsonEqual(rolled.ActiveConfig, initial.LastKnownGoodConfig) {
		t.Fatalf("rollback=%+v", rolled)
	}
	if count, err := store.DecisionCount(t.Context(), scope); err != nil || count != 2 {
		t.Fatalf("decision count=%d err=%v", count, err)
	}

	before := rolled
	_, err = store.Rollback(t.Context(), RolloutRollback{ScopeID: scope, ExpectedVersion: before.Version, Evidence: evidenceFixture(scope, "rollback"), ReasonCode: "duplicate_evidence"})
	if err == nil {
		t.Fatal("invalid decision unexpectedly committed")
	}
	after, err := store.Load(t.Context(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if after.Version != before.Version {
		t.Fatalf("failed transaction changed state: before=%d after=%d", before.Version, after.Version)
	}
	if count, err := store.DecisionCount(t.Context(), scope); err != nil || count != 2 {
		t.Fatalf("failed transaction changed ledger: count=%d err=%v", count, err)
	}
}

// TestEnsureDisabledDefaultSeedsFreshScope proves the fresh-DB crash-loop fix: against
// a freshly-migrated rollout table with NO row for scope, EnsureDisabledDefault (a)
// creates a disabled row, (b) is idempotent on a second call (no error, same version),
// and (c) after seeding, both PersistedCompactionReader.Read (the chat_boot.go
// preflight) and store.Load (what the serve.go evaluator Run loop calls every minute)
// succeed against the seeded scope.
func TestEnsureDisabledDefaultSeedsFreshScope(t *testing.T) {
	pool, _, _ := compactionDB(t)
	scope := "bootstrap-" + uuid.NewString()
	store := NewCompactionRolloutStore(pool)

	if _, err := store.Load(t.Context(), scope); err == nil {
		t.Fatal("scope must have no row before seeding")
	}

	seeded, err := store.EnsureDisabledDefault(t.Context(), scope)
	if err != nil {
		t.Fatalf("EnsureDisabledDefault first call: %v", err)
	}
	if seeded.ScopeID != scope || seeded.Stage != "disabled" || seeded.Version != 1 {
		t.Fatalf("seeded=%+v", seeded)
	}

	again, err := store.EnsureDisabledDefault(t.Context(), scope)
	if err != nil {
		t.Fatalf("EnsureDisabledDefault second call must be idempotent: %v", err)
	}
	if again.Version != seeded.Version || again.Stage != seeded.Stage {
		t.Fatalf("idempotent reseed changed state: first=%+v second=%+v", seeded, again)
	}

	reader := config.PersistedCompactionReader{Source: store, ScopeID: scope}
	snapshot, err := reader.Read(t.Context())
	if err != nil {
		t.Fatalf("preflight Read after seeding must succeed: %v", err)
	}
	if snapshot.Config.Mode != config.CompactionDisabled {
		t.Fatalf("seeded config mode=%q, want disabled", snapshot.Config.Mode)
	}

	if _, err := store.Load(t.Context(), scope); err != nil {
		t.Fatalf("Load after seeding (evaluator Run loop path) must succeed: %v", err)
	}
}

// TestEvaluatorSurvivesDisabledDefaultNoObservation is the BUG-6a / Wave 0.1 regression.
// A freshly-seeded disabled-default scope carries '{}' for all four evaluation windows.
// Before the fix, EvaluateOnce read L0Retention=0 from the empty stratum, tripped a
// phantom safety_gate_failed rollback, and then crashed on the next tick with a
// duplicate-key 23505 (identical Decision digest) — killing the evaluator goroutine for
// the process lifetime. The no-observation guard must make repeated ticks a no-op: no
// error, no rollback decision recorded, version unchanged.
func TestEvaluatorSurvivesDisabledDefaultNoObservation(t *testing.T) {
	pool, _, _ := compactionDB(t)
	scope := "noobs-" + uuid.NewString()
	store := NewCompactionRolloutStore(pool)
	if _, err := store.EnsureDisabledDefault(t.Context(), scope); err != nil {
		t.Fatalf("seed disabled default: %v", err)
	}
	controller := NewCompactionRolloutController(store, scope, time.Now)

	for tick := 1; tick <= 3; tick++ {
		s, err := controller.EvaluateOnce(t.Context())
		if err != nil {
			t.Fatalf("tick %d: EvaluateOnce errored (the evaluator would have died here): %v", tick, err)
		}
		if s.Stage != "disabled" || s.Version != 1 {
			t.Fatalf("tick %d: an unpopulated scope was mutated: stage=%q version=%d, want disabled/1", tick, s.Stage, s.Version)
		}
	}
	if count, err := store.DecisionCount(t.Context(), scope); err != nil || count != 0 {
		t.Fatalf("no-observation ticks recorded a decision: count=%d err=%v, want 0 (no phantom rollback)", count, err)
	}
}

func jsonEqual(left, right []byte) bool {
	var a, b any
	return json.Unmarshal(left, &a) == nil && json.Unmarshal(right, &b) == nil && fmt.Sprint(a) == fmt.Sprint(b)
}

func rolloutFixture(scope string, version int64, stage string) RolloutState {
	return RolloutState{ScopeID: scope, Version: version, Stage: stage, StageStartedAt: time.Now().UTC(), EvaluatorVersion: "eval-v1", ScorerVersion: "score-v1", ConfigVersion: "config-v1", CorpusVersion: "corpus-v1", StratumSnapshots: []byte(`{"tenant":{"attempts":20}}`), FailureWindow: []byte(`{"rate":0}`), LatencyWindow: []byte(`{"p95_ms":12}`), RestoreWindow: []byte(`{"rate":0}`), ActiveConfig: []byte(`{"stage":"disabled"}`), LastKnownGoodConfig: []byte(`{"stage":"disabled","pointer_policy":"lkg"}`), LastKnownGoodPolicy: []byte(`{"pointer":"lkg"}`)}
}

func evidenceFixture(scope, salt string) RolloutEvidence {
	digest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if salt == "rollback" {
		digest = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	}
	if salt == "invalid" {
		digest = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	}
	return RolloutEvidence{ScopeID: scope, Digest: digest, EvaluatorVersion: "eval-v1", ScorerVersion: "score-v1", ConfigVersion: "config-v1", CorpusVersion: "corpus-v1", Snapshot: []byte(`{"quality":"pass"}`)}
}

func transitionFixture(current RolloutState, stage string) RolloutTransition {
	next := current
	next.Stage = stage
	next.StageStartedAt = time.Now().UTC()
	next.ActiveConfig = []byte(`{"stage":"` + stage + `"}`)
	return RolloutTransition{ExpectedVersion: current.Version, State: next, Evidence: evidenceFixture(current.ScopeID, stage), ReasonCode: "quality_gate_passed"}
}
