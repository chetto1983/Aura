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

// TestWindowsUnpopulated locks the Wave 0.1 no-observation guard: only an all-four-empty
// window set (the seed / post-rollback '{}' state) counts as "no observation"; any single
// populated window means real telemetry landed and the scope must still be evaluated.
func TestWindowsUnpopulated(t *testing.T) {
	empty, populated := []byte(`{}`), []byte(`{"rate":0}`)
	cases := []struct {
		name string
		s    RolloutState
		want bool
	}{
		{"all four empty (seed / post-rollback)", RolloutState{StratumSnapshots: empty, FailureWindow: empty, LatencyWindow: empty, RestoreWindow: empty}, true},
		{"all four nil", RolloutState{}, true},
		{"stratum populated", RolloutState{StratumSnapshots: populated, FailureWindow: empty, LatencyWindow: empty, RestoreWindow: empty}, false},
		{"restore populated", RolloutState{StratumSnapshots: empty, FailureWindow: empty, LatencyWindow: empty, RestoreWindow: populated}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := windowsUnpopulated(tc.s); got != tc.want {
				t.Fatalf("windowsUnpopulated = %v, want %v", got, tc.want)
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

// countingRolloutStore signals each Load so a test can deterministically prove the
// evaluator loop kept ticking past a per-tick error instead of returning on the first
// one — without relying on wall-clock timing (which flaked under CI load).
type countingRolloutStore struct {
	fakeRolloutStore
	loads chan struct{}
}

func (c *countingRolloutStore) Load(ctx context.Context, scope string) (RolloutState, error) {
	select {
	case c.loads <- struct{}{}:
	default: // never block the loop if the test has stopped draining
	}
	return c.fakeRolloutStore.Load(ctx, scope)
}

// TestRunSelfHealsOnEvaluationError proves a transient evaluation error (here a corrupt
// window that fails EvaluateOnce every tick) no longer terminates the evaluator loop
// (Wave 0.2). Previously Run returned the error, the detached goroutine logged
// "evaluator stopped" once, and the whole rollout control plane froze for the process
// lifetime. Now Run logs and retries on the next tick and returns nil only on ctx
// cancellation. It must reach a SECOND tick (proving it retried past the first errored
// tick) — asserted deterministically by waiting for two real Load signals, not a sleep.
func TestRunSelfHealsOnEvaluationError(t *testing.T) {
	state := rolloutControllerState(time.Now().Add(-time.Hour), 10)
	state.FailureWindow = []byte(`not-json`) // corrupt → EvaluateOnce errors every tick
	store := &countingRolloutStore{fakeRolloutStore: fakeRolloutStore{state: state}, loads: make(chan struct{}, 64)}
	c := NewCompactionRolloutController(store, "scope", time.Now)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx, time.Millisecond) }()
	for i := 1; i <= 2; i++ {
		select {
		case <-store.loads:
		case <-time.After(5 * time.Second):
			t.Fatalf("evaluator did not reach tick %d — it terminated on an errored tick instead of retrying", i)
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() after repeated eval errors = %v, want nil (self-heal, return only on cancel)", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancellation — it must not block or die on the error path")
	}
}
