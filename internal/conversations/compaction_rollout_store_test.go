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
