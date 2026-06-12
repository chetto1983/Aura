package reasoninglearn

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/prompt"
)

type fakeOracle struct {
	tier  prompt.ReasoningTier
	ok    bool
	calls atomic.Int64
}

func (o *fakeOracle) Label(_ context.Context, _ string) (prompt.ReasoningTier, bool) {
	o.calls.Add(1)
	return o.tier, o.ok
}

type fakeSaver struct {
	mu    sync.Mutex
	saved []string
	err   error
}

func (s *fakeSaver) Save(_ context.Context, text string, _ []float64, _ prompt.ReasoningTier) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	s.saved = append(s.saved, text)
	return nil
}

func (s *fakeSaver) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.saved)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within deadline")
}

func TestLearner_LabelsAndSavesUncertain(t *testing.T) {
	oracle := &fakeOracle{tier: prompt.ReasoningTierNone, ok: true}
	saver := &fakeSaver{}
	var refreshed atomic.Int64
	l := New(Config{Oracle: oracle, Saver: saver, Refresh: func() { refreshed.Add(1) }, MarginFloor: 0.05})
	defer l.Close()

	l.Observe("qual e la capitale dell'Italia?", []float64{0.1, 0.2}, 0.01) // uncertain
	waitFor(t, func() bool { return saver.count() == 1 })
	if got := saver.saved[0]; got != "qual e la capitale dell'Italia?" {
		t.Errorf("saved %q", got)
	}
	if refreshed.Load() == 0 {
		t.Error("refresh not called after save")
	}
}

func TestLearner_SkipsConfidentAndDedups(t *testing.T) {
	oracle := &fakeOracle{tier: prompt.ReasoningTierHigh, ok: true}
	saver := &fakeSaver{}
	l := New(Config{Oracle: oracle, Saver: saver, MarginFloor: 0.05})
	defer l.Close()

	l.Observe("confident", []float64{1, 0}, 0.20) // above floor → ignored
	// Same uncertain text observed twice → labeled once.
	l.Observe("dup", []float64{0, 1}, 0.01)
	l.Observe("dup", []float64{0, 1}, 0.02)
	waitFor(t, func() bool { return saver.count() == 1 })
	time.Sleep(50 * time.Millisecond) // allow any erroneous extra to land
	if saver.count() != 1 {
		t.Fatalf("want 1 save (confident skipped, dup deduped), got %d", saver.count())
	}
	if oracle.calls.Load() != 1 {
		t.Errorf("oracle called %d times, want 1", oracle.calls.Load())
	}
}

func TestLearner_OracleMissDoesNotSave(t *testing.T) {
	oracle := &fakeOracle{ok: false}
	saver := &fakeSaver{}
	l := New(Config{Oracle: oracle, Saver: saver, MarginFloor: 0.05})
	defer l.Close()

	l.Observe("ambiguous", []float64{0.5, 0.5}, 0.01)
	waitFor(t, func() bool { return oracle.calls.Load() == 1 })
	time.Sleep(50 * time.Millisecond)
	if saver.count() != 0 {
		t.Errorf("oracle miss must not save; got %d", saver.count())
	}
}

func TestNew_NilWhenIncomplete(t *testing.T) {
	if New(Config{Saver: &fakeSaver{}}) != nil {
		t.Error("New without oracle should be nil")
	}
	if New(Config{Oracle: &fakeOracle{}}) != nil {
		t.Error("New without saver should be nil")
	}
	// A nil learner's methods are safe no-ops.
	var l *Learner
	l.Observe("x", nil, 0)
	l.Close()
}
