// Package attempts persists and retrieves tool-call observations for
// the Phase-6 Tool Experience Loop (US-J02).
package attempts

import (
	"context"
	"errors"
	"time"

	tools "github.com/aura/aura/internal/agent/tools/registry"
)

// ErrRepoUnavailable is returned by SQLiteRepo when the underlying DB is nil.
var ErrRepoUnavailable = errors.New("attempts: repo unavailable")

// ToolAttempt is a single row from tool_attempts as returned by Recent.
type ToolAttempt struct {
	ID            string
	RunID         string
	ToolName      string
	ToolKind      string
	Outcome       string
	Class         string
	Reason        string
	ErrorRedacted string
	ElapsedMS     int64
	StartedAt     time.Time
	EndedAt       time.Time
	AttemptN      uint16
}

// Repo persists and retrieves tool-call observations.
type Repo interface {
	// Record writes one ToolObservation synchronously inside BEGIN IMMEDIATE.
	// Returns ErrRepoUnavailable when the underlying DB is nil.
	Record(ctx context.Context, obs tools.ToolObservation) error

	// Recent returns up to n most-recent attempts for toolName within runID,
	// ordered newest first (by ended_at DESC).
	Recent(ctx context.Context, runID, toolName string, n int) ([]ToolAttempt, error)
}
