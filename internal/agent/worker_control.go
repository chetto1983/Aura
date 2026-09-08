package agent

import (
	"context"
	"errors"
)

// ErrWorkerStopped distinguishes an operator stop from retryable execution failure.
var ErrWorkerStopped = errors.New("worker stopped by operator")

// WorkerControlParams binds host callbacks to a child without granting parent control.
type WorkerControlParams struct {
	ConversationID string
	ChildID        string
	SteerEnabled   bool
	Cancel         context.CancelFunc
	Stop           func(context.Context) error
	OnClose        func() error
}

// WorkerControlSession identifies one incarnation and settles its pending controls.
type WorkerControlSession struct {
	RunID  string
	Finish func() error
}

// WorkerRuntime shares the host's owner-scoped run registry with headless workers.
// The transcript producer stays in the swarm runner, not in a second agent loop.
type WorkerRuntime interface {
	StartWorker(context.Context, WorkerControlParams) (WorkerControlSession, error)
}
