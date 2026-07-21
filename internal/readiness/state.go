// Package readiness owns the daemon's in-process readiness snapshot.
package readiness

import "time"

// Code is a stable, Aura-owned public readiness reason code.
type Code string

const (
	// CodePostgresUnavailable reports a failed PostgreSQL readiness probe.
	CodePostgresUnavailable Code = "postgres_unavailable"
	// CodeNeo4jUnavailable reports a failed Neo4j readiness probe.
	CodeNeo4jUnavailable Code = "neo4j_unavailable"
	// CodeListenerUnavailable reports a listener that is not serving.
	CodeListenerUnavailable Code = "listener_unavailable"
	// CodeMigrationIncompatible reports a runtime schema that is not at head.
	CodeMigrationIncompatible Code = "migration_incompatible"
	// CodeSchedulerStalled reports enabled scheduler progress outside its freshness window.
	CodeSchedulerStalled Code = "scheduler_stalled"
	// CodeDraining reports that ordered process shutdown has begun.
	CodeDraining Code = "draining"
)

// Config controls snapshot defaults and time-based scheduler freshness.
type Config struct {
	Now                 func() time.Time
	SchedulerMaxAge     time.Duration
	MigrationCompatible bool
	SchedulerEnabled    bool
}

// Snapshot is the concurrency-safe daemon lifecycle and progress snapshot.
// The implementation is completed in the GREEN step for plan 39-03.
type Snapshot struct{}

// NewSnapshot builds a snapshot from the supplied runtime configuration.
func NewSnapshot(Config) *Snapshot { return &Snapshot{} }

// MarkListenerBound records successful synchronous listener binding.
func (*Snapshot) MarkListenerBound() {}

// MarkListenerRunning records entry into the HTTP serve loop.
func (*Snapshot) MarkListenerRunning() {}

// MarkListenerFailure records a terminal listener failure.
func (*Snapshot) MarkListenerFailure(error) {}

// MarkDraining records the irreversible start of process drain.
func (*Snapshot) MarkDraining() {}

// MarkMigrationCompatible records whether the runtime schema is at head.
func (*Snapshot) MarkMigrationCompatible(bool) {}

// SetSchedulerEnabled records whether scheduler progress is required.
func (*Snapshot) SetSchedulerEnabled(bool) {}

// MarkSchedulerProgress records a successfully completed scheduler scan.
func (*Snapshot) MarkSchedulerProgress() {}

// MarkSchedulerFailure records a terminal scheduler loop failure.
func (*Snapshot) MarkSchedulerFailure(error) {}

// Reasons returns sorted stable reason codes for current readiness failures.
func (*Snapshot) Reasons() []Code { return nil }
