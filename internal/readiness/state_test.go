package readiness

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestSnapshotListenerLifecycle(t *testing.T) {
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	s := NewSnapshot(Config{Now: func() time.Time { return now }, MigrationCompatible: true})

	assertReasons(t, s, CodeListenerUnavailable)
	s.MarkListenerBound()
	assertReasons(t, s, CodeListenerUnavailable)
	s.MarkListenerRunning()
	assertReasons(t, s)
	s.MarkListenerFailure(errors.New("accept failed: secret-listener-detail"))
	assertReasons(t, s, CodeListenerUnavailable)
}

func TestSnapshotDrainAndMigrationStates(t *testing.T) {
	s := NewSnapshot(Config{MigrationCompatible: false})
	s.MarkListenerRunning()
	assertReasons(t, s, CodeMigrationIncompatible)

	s.MarkMigrationCompatible(true)
	assertReasons(t, s)
	s.MarkDraining()
	assertReasons(t, s, CodeDraining)
}

func TestSnapshotSchedulerFreshnessBoundaries(t *testing.T) {
	base := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	now := base
	maxAge := 90 * time.Second
	s := NewSnapshot(Config{
		Now:                 func() time.Time { return now },
		SchedulerMaxAge:     maxAge,
		MigrationCompatible: true,
		SchedulerEnabled:    false,
	})
	s.MarkListenerRunning()

	assertReasons(t, s) // disabled is healthy by configuration
	s.SetSchedulerEnabled(true)
	assertReasons(t, s, CodeSchedulerStalled)
	s.MarkSchedulerProgress()

	now = base.Add(maxAge - time.Nanosecond)
	assertReasons(t, s)
	now = base.Add(maxAge)
	assertReasons(t, s)
	now = base.Add(maxAge + time.Nanosecond)
	assertReasons(t, s, CodeSchedulerStalled)

	now = base.Add(2 * maxAge)
	s.MarkSchedulerProgress()
	assertReasons(t, s)
	s.MarkSchedulerFailure(errors.New("terminal scheduler failure"))
	assertReasons(t, s, CodeSchedulerStalled)
}

func TestSnapshotReasonsAreSortedAndRaceSafe(t *testing.T) {
	s := NewSnapshot(Config{SchedulerEnabled: true})
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			s.MarkListenerBound()
			s.MarkMigrationCompatible(false)
			s.MarkSchedulerProgress()
			_ = s.Reasons()
		})
	}
	wg.Wait()
	s.MarkDraining()
	s.MarkSchedulerFailure(errors.New("terminal"))
	assertReasons(t, s,
		CodeDraining,
		CodeListenerUnavailable,
		CodeMigrationIncompatible,
		CodeSchedulerStalled,
	)
}

func TestSnapshotSetSchedulerMaxAge(t *testing.T) {
	base := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	now := base
	s := NewSnapshot(Config{
		Now:                 func() time.Time { return now },
		SchedulerMaxAge:     90 * time.Second,
		MigrationCompatible: true,
		SchedulerEnabled:    true,
	})
	s.MarkListenerRunning()
	s.MarkSchedulerProgress()

	now = base.Add(150 * time.Second) // stale under the 90s default, fresh under 300s
	assertReasons(t, s, CodeSchedulerStalled)

	s.SetSchedulerMaxAge(300 * time.Second)
	assertReasons(t, s)

	// Non-positive values are ignored, keeping the current (widened) window.
	s.SetSchedulerMaxAge(0)
	assertReasons(t, s)
	s.SetSchedulerMaxAge(-time.Second)
	assertReasons(t, s)

	now = base.Add(400 * time.Second)
	assertReasons(t, s, CodeSchedulerStalled)
}

func assertReasons(t *testing.T, s *Snapshot, want ...Code) {
	t.Helper()
	if got := s.Reasons(); !slices.Equal(got, want) {
		t.Fatalf("Reasons() = %v, want %v", got, want)
	}
}
