// Package readiness owns the daemon's in-process readiness snapshot.
package readiness

import (
	"slices"
	"sync"
	"time"
)

// Code is a stable, Aura-owned public readiness reason code.
type Code string

const (
	// CodePostgresUnavailable reports a failed PostgreSQL readiness probe.
	CodePostgresUnavailable Code = "postgres_unavailable"
	// CodeMemoryUnavailable reports a failed required Agent Memory capability probe.
	CodeMemoryUnavailable Code = "memory_unavailable"
	// CodeSandboxUnavailable reports a per-identity sandbox that cannot be resolved. Since the
	// box is the only place shell_exec and the fs_* tools run, this means the agent can answer
	// but cannot DO anything — the daemon must say so rather than report itself healthy.
	CodeSandboxUnavailable Code = "sandbox_unavailable"
	// CodeListenerUnavailable reports a listener that is not serving.
	CodeListenerUnavailable Code = "listener_unavailable"
	// CodeMigrationIncompatible reports a runtime schema that is not at head.
	CodeMigrationIncompatible Code = "migration_incompatible"
	// CodeSchedulerStalled reports enabled scheduler progress outside its freshness window.
	CodeSchedulerStalled Code = "scheduler_stalled"
	// CodeDraining reports that ordered process shutdown has begun.
	CodeDraining Code = "draining"
	// CodeDependencyUnavailable is the bounded fallback for an unclassified required probe.
	CodeDependencyUnavailable Code = "dependency_unavailable"
)

const defaultSchedulerMaxAge = 90 * time.Second

// Config controls snapshot defaults and time-based scheduler freshness.
type Config struct {
	Now                 func() time.Time
	SchedulerMaxAge     time.Duration
	MigrationCompatible bool
	SchedulerEnabled    bool
}

// Snapshot is the concurrency-safe daemon lifecycle and progress snapshot.
type Snapshot struct {
	now             func() time.Time
	schedulerMaxAge time.Duration

	mu                    sync.RWMutex
	listenerBound         bool
	listenerRunning       bool
	listenerFailure       error
	draining              bool
	migrationCompatible   bool
	schedulerEnabled      bool
	schedulerLastProgress time.Time
	schedulerFailure      error
}

// NewSnapshot builds a snapshot from the supplied runtime configuration.
func NewSnapshot(cfg Config) *Snapshot {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	maxAge := cfg.SchedulerMaxAge
	if maxAge <= 0 {
		maxAge = defaultSchedulerMaxAge
	}
	return &Snapshot{
		now:                 now,
		schedulerMaxAge:     maxAge,
		migrationCompatible: cfg.MigrationCompatible,
		schedulerEnabled:    cfg.SchedulerEnabled,
	}
}

// MarkListenerBound records successful synchronous listener binding.
func (s *Snapshot) MarkListenerBound() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listenerFailure == nil {
		s.listenerBound = true
	}
}

// MarkListenerRunning records entry into the HTTP serve loop.
func (s *Snapshot) MarkListenerRunning() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listenerFailure == nil {
		s.listenerBound = true
		s.listenerRunning = true
	}
}

// MarkListenerFailure records a terminal listener failure.
func (s *Snapshot) MarkListenerFailure(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil {
		return
	}
	s.listenerFailure = err
	s.listenerRunning = false
}

// MarkDraining records the irreversible start of process drain.
func (s *Snapshot) MarkDraining() {
	s.mu.Lock()
	s.draining = true
	s.mu.Unlock()
}

// IsDraining reports whether ordered process shutdown has begun.
func (s *Snapshot) IsDraining() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.draining
}

// MarkMigrationCompatible records whether the runtime schema is at head.
func (s *Snapshot) MarkMigrationCompatible(compatible bool) {
	s.mu.Lock()
	s.migrationCompatible = compatible
	s.mu.Unlock()
}

// SetSchedulerEnabled records whether scheduler progress is required.
func (s *Snapshot) SetSchedulerEnabled(enabled bool) {
	s.mu.Lock()
	s.schedulerEnabled = enabled
	s.mu.Unlock()
}

// SetSchedulerMaxAge widens or narrows the scheduler staleness window (e.g. to
// track an operator-configured tick interval). d<=0 is ignored, keeping the
// current value — NewSnapshot's defaultSchedulerMaxAge floor stays intact.
func (s *Snapshot) SetSchedulerMaxAge(d time.Duration) {
	if d <= 0 {
		return
	}
	s.mu.Lock()
	s.schedulerMaxAge = d
	s.mu.Unlock()
}

// MarkSchedulerProgress records a successfully completed scheduler scan.
func (s *Snapshot) MarkSchedulerProgress() {
	now := s.now().UTC()
	s.mu.Lock()
	if now.After(s.schedulerLastProgress) {
		s.schedulerLastProgress = now
	}
	s.mu.Unlock()
}

// MarkSchedulerFailure records a terminal scheduler loop failure.
func (s *Snapshot) MarkSchedulerFailure(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.schedulerFailure = err
	}
}

// Reasons returns sorted stable reason codes for current readiness failures.
func (s *Snapshot) Reasons() []Code {
	now := s.now().UTC()
	s.mu.RLock()
	listenerReady := s.listenerBound && s.listenerRunning && s.listenerFailure == nil
	draining := s.draining
	migrationCompatible := s.migrationCompatible
	schedulerEnabled := s.schedulerEnabled
	schedulerProgress := s.schedulerLastProgress
	schedulerFailed := s.schedulerFailure != nil
	s.mu.RUnlock()

	reasons := make([]Code, 0, 4)
	if draining {
		reasons = append(reasons, CodeDraining)
	}
	if !listenerReady {
		reasons = append(reasons, CodeListenerUnavailable)
	}
	if !migrationCompatible {
		reasons = append(reasons, CodeMigrationIncompatible)
	}
	if schedulerEnabled && (schedulerFailed || schedulerProgress.IsZero() || now.Sub(schedulerProgress) > s.schedulerMaxAge) {
		reasons = append(reasons, CodeSchedulerStalled)
	}
	slices.Sort(reasons)
	return reasons
}
