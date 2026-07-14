package conversations

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/conversations/compaction_eval"
)

// passingGates is a gate snapshot that clears every promotion AND rollback threshold;
// individual test cases mutate one field at a time to trip exactly one rollbackReason.
func passingGates() compaction_eval.Gates {
	return compaction_eval.Gates{
		L0Retention: 1, ToolPendingRetention: .999, FactualDecisionRetention: .99,
		ContinuationDelta: 0, ContinuationConfidence: .95, MedianReduction: .5, TargetAchievement: .999,
		FailureRate: .005, CostRatio: .1, P95ProactiveSeconds: 4, P95OverflowSeconds: 8,
	}
}

func rollbackDecision(s RolloutState, mutate func(*compaction_eval.Gates)) compaction_eval.Decision {
	g := passingGates()
	if mutate != nil {
		mutate(&g)
	}
	return compaction_eval.Decision{ScopeID: s.ScopeID, EligibleAttempts: s.EligibleAttempts, EvaluatorVersion: s.EvaluatorVersion, ScorerVersion: s.ScorerVersion, ConfigVersion: s.ConfigVersion, CorpusVersion: s.CorpusVersion, Gates: g}
}

func TestRollbackReasonBranches(t *testing.T) {
	s := rolloutControllerState(time.Now().Add(-48*time.Hour), 2000)
	cases := []struct {
		name   string
		mutate func(d *compaction_eval.Decision)
		want   string
	}{
		{"stale evaluator version", func(d *compaction_eval.Decision) { d.EvaluatorVersion = "eval-v0" }, "stale_evaluator_version"},
		{"stale scorer version", func(d *compaction_eval.Decision) { d.ScorerVersion = "score-v0" }, "incompatible_evidence_version"},
		{"stale config version", func(d *compaction_eval.Decision) { d.ConfigVersion = "config-v0" }, "incompatible_evidence_version"},
		{"stale corpus version", func(d *compaction_eval.Decision) { d.CorpusVersion = "corpus-v0" }, "incompatible_evidence_version"},
		{"corrupt evidence", func(d *compaction_eval.Decision) { d.Gates.CorruptEvidence = 1 }, "corrupt_evidence"},
		{"authority escalation", func(d *compaction_eval.Decision) { d.Gates.AuthorityEscalations = 1 }, "safety_gate_failed"},
		{"l0 retention below one", func(d *compaction_eval.Decision) { d.Gates.L0Retention = .999 }, "safety_gate_failed"},
		{"tool pending retention below floor", func(d *compaction_eval.Decision) { d.Gates.ToolPendingRetention = .5 }, "continuation_gate_failed"},
		{"factual decision retention below floor", func(d *compaction_eval.Decision) { d.Gates.FactualDecisionRetention = .5 }, "continuation_gate_failed"},
		{"continuation delta regressed", func(d *compaction_eval.Decision) { d.Gates.ContinuationDelta = -0.5 }, "continuation_gate_failed"},
		{"continuation confidence below floor", func(d *compaction_eval.Decision) { d.Gates.ContinuationConfidence = .1 }, "continuation_gate_failed"},
		{"failure rate exceeded", func(d *compaction_eval.Decision) { d.Gates.FailureRate = .5 }, "failure_window_exceeded"},
		{"latency window exceeded", func(d *compaction_eval.Decision) { d.Gates.LatencyBreachMinutes = 30 }, "latency_window_exceeded"},
		{"restore rate exceeded", func(d *compaction_eval.Decision) { d.Gates.RestoreRate = .5 }, "restore_rate_exceeded"},
		{"every gate passes", func(d *compaction_eval.Decision) {}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := rollbackDecision(s, nil)
			tc.mutate(&d)
			if got := rollbackReason(s, d); got != tc.want {
				t.Fatalf("rollbackReason() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRunEvaluatesImmediatelyThenStopsOnCancel proves Run's evaluate-then-wait loop:
// an immediate first EvaluateOnce (no promotion, stable state) followed by cancellation
// during the interval wait returns nil rather than blocking forever.
func TestRunEvaluatesImmediatelyThenStopsOnCancel(t *testing.T) {
	state := rolloutControllerState(time.Now().Add(-time.Hour), 10)
	store := &fakeRolloutStore{state: state}
	c := NewCompactionRolloutController(store, "scope", time.Now)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx, time.Millisecond) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() err=%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

// erroringRolloutStore always fails Load, isolating the controller's error-propagation
// branches (Apply/EvaluateOnce) from the transition/rollback plumbing fakeRolloutStore
// already exercises.
type erroringRolloutStore struct{ loadErr error }

func (e *erroringRolloutStore) Load(context.Context, string) (RolloutState, error) {
	return RolloutState{}, e.loadErr
}
func (e *erroringRolloutStore) Transition(context.Context, RolloutTransition) (RolloutState, error) {
	panic("Transition must not be called when Load fails")
}
func (e *erroringRolloutStore) Rollback(context.Context, RolloutRollback) (RolloutState, error) {
	panic("Rollback must not be called when Load fails")
}

func TestNewCompactionRolloutControllerDefaultsNowWhenNil(t *testing.T) {
	c := NewCompactionRolloutController(&fakeRolloutStore{}, "scope", nil)
	before := time.Now()
	got := c.now()
	if got.Before(before.Add(-time.Second)) || got.After(time.Now().Add(time.Second)) {
		t.Fatalf("default now() = %v, want close to real time", got)
	}
}

func TestApplyRejectsUnconfiguredController(t *testing.T) {
	cases := []struct {
		name string
		c    *CompactionRolloutController
	}{
		{"nil controller", nil},
		{"nil store", &CompactionRolloutController{scope: "scope", now: time.Now}},
		{"empty scope", &CompactionRolloutController{store: &fakeRolloutStore{}, now: time.Now}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.c.Apply(context.Background(), compaction_eval.Decision{}); err == nil {
				t.Fatal("expected unavailable-controller error")
			}
		})
	}
}

func TestApplyPropagatesLoadError(t *testing.T) {
	wantErr := errors.New("load failed")
	c := NewCompactionRolloutController(&erroringRolloutStore{loadErr: wantErr}, "scope", time.Now)
	if _, err := c.Apply(context.Background(), compaction_eval.Decision{}); !errors.Is(err, wantErr) {
		t.Fatalf("err=%v", err)
	}
}

func TestEvaluateOncePropagatesLoadError(t *testing.T) {
	wantErr := errors.New("load failed")
	c := NewCompactionRolloutController(&erroringRolloutStore{loadErr: wantErr}, "scope", time.Now)
	if _, err := c.EvaluateOnce(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("err=%v", err)
	}
}

// TestRunPropagatesEvaluationErrorAndDefaultsInterval proves Run surfaces a corrupt
// evaluation window immediately (no promotion decision reached) and exercises the
// interval<=0 default-to-one-minute branch without actually waiting a minute (the
// evaluation error returns before the ticker is ever created).
func TestRunPropagatesEvaluationErrorAndDefaultsInterval(t *testing.T) {
	state := rolloutControllerState(time.Now().Add(-time.Hour), 10)
	state.FailureWindow = []byte(`not-json`)
	store := &fakeRolloutStore{state: state}
	c := NewCompactionRolloutController(store, "scope", time.Now)
	if err := c.Run(context.Background(), 0); err == nil {
		t.Fatal("expected corrupt-window evaluation error")
	}
}
