package runner

import (
	"context"
	"fmt"
	"iter"
	"time"

	"github.com/chetto1983/aura/internal/agent"
)

// SteerPusher is the producer half of the shared steer store. It stays separate
// from agent.SteerInbox's Drain-only contract: ordinary agents can consume but
// only composition-owned runtime dispatchers can manufacture a wake.
type SteerPusher interface {
	Push(conv, source, text string) error
}

// WakeWithSteer serialises an internally-generated steer with the conversation's
// ordinary Runner turns. The lock is acquired before Push: an ending foreground
// turn therefore cannot drain this row and leave a separately scheduled empty
// wake behind. Once pushed, the ordinary LlmAgent start-of-round drain owns the
// message, persistence and attribution envelope.
func (r *Runner) WakeWithSteer(
	ctx context.Context,
	convID string,
	pusher SteerPusher,
	source string,
	text string,
) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		if pusher == nil {
			yield(nil, fmt.Errorf("wake with steer: pusher is not configured"))
			return
		}
		scopedCtx, err := r.scopeContextToConversation(ctx, convID)
		if err != nil {
			yield(nil, err)
			return
		}
		lockedCtx := scopedCtx
		if !threadLockHeld(scopedCtx) {
			unlock, err := r.LockThread(scopedCtx, convID)
			if err != nil {
				yield(nil, err)
				return
			}
			defer unlock()
			lockedCtx = WithThreadLockHeld(scopedCtx)
		}
		if err := lockedCtx.Err(); err != nil {
			yield(nil, err)
			return
		}
		if err := pusher.Push(convID, source, text); err != nil {
			yield(nil, fmt.Errorf("wake with steer: push: %w", err))
			return
		}
		inner := r.turnLocked(lockedCtx, convID, turnInput{})
		for ev, runErr := range r.deliverLeftoverSteer(lockedCtx, convID, inner) {
			if !yield(ev, runErr) {
				return
			}
		}
	}
}

// LockThread waits for the conversation's run lock until ctx ends: the wake's
// counterpart to TryLockThread, which an operator's run uses to answer 409 instead.
// A wake queues behind the turn in progress, and still drains cleanly on daemon
// shutdown while a foreground turn owns the lock. A host that registers the wake as
// a run (agui) takes the lock here and hands WakeWithSteer the ctx that says so.
func (r *Runner) LockThread(ctx context.Context, convID string) (func(), error) {
	mu := r.lockForThread(ctx, convID)
	if mu.TryLock() {
		return mu.Unlock, nil
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			if mu.TryLock() {
				return mu.Unlock, nil
			}
		}
	}
}
