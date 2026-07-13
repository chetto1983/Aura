package conversations

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/conversations/compaction_eval"
)

type fakeRolloutStore struct {
	state                  RolloutState
	transitions, rollbacks int
	conflict               bool
}

func (f *fakeRolloutStore) Load(context.Context, string) (RolloutState, error) { return f.state, nil }
func (f *fakeRolloutStore) Transition(_ context.Context, c RolloutTransition) (RolloutState, error) {
	if f.conflict {
		return RolloutState{}, ErrRolloutStaleVersion
	}
	f.transitions++
	c.State.Version++
	f.state = c.State
	return f.state, nil
}
func (f *fakeRolloutStore) Rollback(_ context.Context, r RolloutRollback) (RolloutState, error) {
	if f.conflict {
		return RolloutState{}, ErrRolloutStaleVersion
	}
	f.rollbacks++
	f.state.Version++
	f.state.Stage = "disabled"
	f.state.ActiveConfig = f.state.LastKnownGoodConfig
	return f.state, nil
}

func TestPromotionAfter24HoursAnd1000Attempts(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	state := rolloutControllerState(now.Add(-24*time.Hour), 1000)
	store := &fakeRolloutStore{state: state}
	c := NewCompactionRolloutController(store, "scope", func() time.Time { return now })
	d := passingDecision(t, state)
	got, err := c.Apply(t.Context(), d)
	if err != nil || got.Stage != "canary_1" || store.transitions != 1 {
		t.Fatalf("got=%+v transitions=%d err=%v", got, store.transitions, err)
	}
}

func TestRolloutControllerRequiresDurationAndAttempts(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name     string
		age      time.Duration
		attempts int64
	}{{"duration", 23 * time.Hour, 1000}, {"attempts", 24 * time.Hour, 999}} {
		t.Run(tc.name, func(t *testing.T) {
			s := rolloutControllerState(now.Add(-tc.age), tc.attempts)
			f := &fakeRolloutStore{state: s}
			_, err := NewCompactionRolloutController(f, "scope", func() time.Time { return now }).Apply(t.Context(), passingDecision(t, s))
			if err != nil || f.transitions != 0 {
				t.Fatalf("transitions=%d err=%v", f.transitions, err)
			}
		})
	}
}

func TestRollbackSafetyAndStaleEvaluator(t *testing.T) {
	s := rolloutControllerState(time.Now().Add(-48*time.Hour), 2000)
	s.Stage = "canary_1"
	f := &fakeRolloutStore{state: s}
	d := passingDecision(t, s)
	d.Gates.AuthorityEscalations = 1
	_, err := NewCompactionRolloutController(f, "scope", time.Now).Apply(t.Context(), d)
	if err != nil || f.rollbacks != 1 {
		t.Fatalf("rollbacks=%d err=%v", f.rollbacks, err)
	}
	f.state = s
	d = passingDecision(t, s)
	d.EvaluatorVersion = "eval-v0"
	_, err = NewCompactionRolloutController(f, "scope", time.Now).Apply(t.Context(), d)
	if err != nil || f.rollbacks != 2 {
		t.Fatalf("stale rollback=%d err=%v", f.rollbacks, err)
	}
}

func TestRolloutControllerConflictIsReported(t *testing.T) {
	s := rolloutControllerState(time.Now().Add(-48*time.Hour), 2000)
	f := &fakeRolloutStore{state: s, conflict: true}
	_, err := NewCompactionRolloutController(f, "scope", time.Now).Apply(t.Context(), passingDecision(t, s))
	if !errors.Is(err, ErrRolloutStaleVersion) {
		t.Fatalf("err=%v", err)
	}
}

func rolloutControllerState(start time.Time, attempts int64) RolloutState {
	return RolloutState{ScopeID: "scope", Version: 1, Stage: "shadow", StageStartedAt: start, EligibleAttempts: attempts, EvaluatorVersion: "eval-v1", ScorerVersion: "score-v1", ConfigVersion: "config-v1", CorpusVersion: "corpus-v1", ActiveConfig: []byte(`{"mode":"shadow","percent":0,"recovery_drill_passed":true}`), LastKnownGoodConfig: []byte(`{"mode":"disabled","percent":0,"recovery_drill_passed":true}`), LastKnownGoodPolicy: []byte(`{"pointer":"lkg"}`), StratumSnapshots: []byte(`{}`), FailureWindow: []byte(`{"rate":0}`), LatencyWindow: []byte(`{"p95_seconds":1,"breach_minutes":0}`), RestoreWindow: []byte(`{"rate":0}`)}
}
func passingDecision(t *testing.T, s RolloutState) compaction_eval.Decision {
	t.Helper()
	d, err := compaction_eval.Evaluate(compaction_eval.Input{ScopeID: s.ScopeID, EligibleAttempts: s.EligibleAttempts, EvaluatorVersion: s.EvaluatorVersion, ScorerVersion: s.ScorerVersion, ConfigVersion: s.ConfigVersion, CorpusVersion: s.CorpusVersion, Gates: compaction_eval.Gates{L0Retention: 1, ToolPendingRetention: .999, FactualDecisionRetention: .99, ContinuationDelta: 0, ContinuationConfidence: .95, MedianReduction: .5, TargetAchievement: .999, FailureRate: .005, CostRatio: .1, P95ProactiveSeconds: 4, P95OverflowSeconds: 8}})
	if err != nil {
		t.Fatal(err)
	}
	return d
}
