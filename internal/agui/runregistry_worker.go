package agui

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/steer"
	"github.com/google/uuid"
)

var errWorkerAlreadyRunning = errors.New("agui: worker execution already registered")

// StartWorker registers a control-only session without replacing parent discovery.
func (r *RunRegistry) StartWorker(ctx context.Context, p agent.WorkerControlParams) (agent.WorkerControlSession, error) {
	if r == nil || ctx == nil || p.Cancel == nil || p.Stop == nil || p.ChildID == "" || len(p.ChildID) > 128 || strings.ContainsAny(p.ChildID, "/\\\x00") {
		return agent.WorkerControlSession{}, steer.ErrWorkerScope
	}
	if err := ctx.Err(); err != nil {
		return agent.WorkerControlSession{}, err
	}
	owner := identityctx.IdentityID(ctx)
	if _, err := uuid.Parse(owner); err != nil {
		return agent.WorkerControlSession{}, steer.ErrWorkerScope
	}
	if _, err := uuid.Parse(p.ConversationID); err != nil {
		return agent.WorkerControlSession{}, steer.ErrWorkerScope
	}
	runID := "run-" + uuid.Must(uuid.NewV7()).String()
	session, err := r.Start(runParams{
		runID: runID, threadID: p.ConversationID, identityID: owner, workerID: p.ChildID,
		cancel: p.Cancel, operatorStop: p.Stop, steerEnabled: p.SteerEnabled,
	})
	if err != nil {
		return agent.WorkerControlSession{}, err
	}
	var once sync.Once
	var finishErr error
	return agent.WorkerControlSession{RunID: runID, Finish: func() error {
		once.Do(func() {
			session.controlMu.Lock()
			defer session.controlMu.Unlock()
			session.finish()
			if p.OnClose != nil {
				finishErr = p.OnClose()
			}
		})
		return finishErr
	}}, nil
}

// LiveForWorker exposes only the exact owner's active child incarnation.
func (r *RunRegistry) LiveForWorker(owner, conv, childID string) (*RunSession, bool) {
	if r == nil || childID == "" {
		return nil, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	session, ok := r.byThread[threadKey{identity: owner, thread: conv, worker: childID}]
	return session, ok
}

func (s *RunSession) withWorkerControl(apply func() error) error {
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	if terminal, _ := s.terminalState(); terminal {
		return steer.ErrClosed
	}
	return apply()
}
