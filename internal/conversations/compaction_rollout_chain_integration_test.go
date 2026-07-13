//go:build db_integration

package conversations

import (
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCompactionRolloutFullChainPostgres(t *testing.T) {
	pool, dsn, _ := compactionDB(t)
	replica, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(replica.Close)
	scope := "chain-" + uuid.NewString()
	initial := rolloutControllerState(time.Now().UTC().Add(-25*time.Hour), 1000)
	initial.ScopeID = scope
	initial.StratumSnapshots = []byte(`{"l0_retention":1,"tool_pending_retention":0.999,"factual_decision_retention":0.99,"continuation_delta":0,"continuation_confidence":0.95,"median_reduction":0.5,"target_achievement":0.999,"cost_ratio":0.1,"p95_overflow_seconds":8}`)
	first, second := NewCompactionRolloutStore(pool), NewCompactionRolloutStore(replica)
	created, err := first.Create(t.Context(), initial)
	if err != nil {
		t.Fatal(err)
	}
	initial.Version = created.Version
	if _, err = first.Transition(t.Context(), RolloutTransition{ExpectedVersion: created.Version, State: initial, Evidence: evidenceFixture(scope, "shadow"), ReasonCode: "shadow_evaluation_started"}); err != nil {
		t.Fatal(err)
	}
	controller := NewCompactionRolloutController(first, scope, time.Now)
	promoted, err := controller.EvaluateOnce(t.Context())
	if err != nil || promoted.Stage != "canary_1" {
		t.Fatalf("promoted=%+v err=%v", promoted, err)
	}
	r1 := config.PersistedCompactionReader{Source: first, ScopeID: scope}
	r2 := config.PersistedCompactionReader{Source: second, ScopeID: scope}
	claim, err := r1.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	seen, err := r2.Read(t.Context())
	if err != nil || seen.Version != claim.Version {
		t.Fatalf("replica=%+v err=%v", seen, err)
	}
	decision := passingDecision(t, promoted)
	decision.ScopeID = scope
	decision.Gates.RestoreRate = .02
	rolled, err := NewCompactionRolloutController(second, scope, time.Now).Apply(t.Context(), decision)
	if err != nil || rolled.Stage != "disabled" {
		t.Fatalf("rolled=%+v err=%v", rolled, err)
	}
	final, err := r1.Read(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if final.Version == claim.Version || final.Config.Mode != config.CompactionDisabled {
		t.Fatalf("stale finalize accepted: claim=%d final=%+v", claim.Version, final)
	}
	if n, err := first.DecisionCount(t.Context(), scope); err != nil || n != 3 {
		t.Fatalf("ledger=%d err=%v", n, err)
	}
	_, err = first.Transition(t.Context(), RolloutTransition{ExpectedVersion: claim.Version, State: promoted, Evidence: evidenceFromDecision(decision), ReasonCode: "stale_finalize"})
	if err == nil || !errors.Is(err, ErrRolloutStaleVersion) {
		t.Fatalf("stale transition err=%v", err)
	}
	pool.Close()
	replica.Close()
	reopened, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	persisted, err := config.PersistedCompactionReader{Source: NewCompactionRolloutStore(reopened), ScopeID: scope}.Read(t.Context())
	if err != nil || persisted.Version != final.Version || persisted.Config.Mode != config.CompactionDisabled {
		t.Fatalf("restart=%+v err=%v", persisted, err)
	}
}
