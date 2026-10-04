package agui

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"

	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/runner"
)

// steerWakeRunner is the runner's runtime wake in its two halves, so a host can hold the
// conversation lock across the run it registers. *runner.Runner satisfies it.
type steerWakeRunner interface {
	LockThread(ctx context.Context, convID string) (func(), error)
	WakeWithSteer(ctx context.Context, convID string, pusher runner.SteerPusher, source, text string) iter.Seq2[*agent.Event, error]
}

// WakeWithSteer runs a runtime wake -- a background shell, video job or tool call finished --
// as a detached run, the way ResumePendingSteer runs a coordinator's continuation. Driven by
// the runner alone, a woken turn reached no open cockpit until a reload (prd.md §12,
// 2026-10-04). As a run it carries a live_run_id and the conversation stream announces it,
// so a tab attaches to it like any run it did not start, and can stop it. Unlike an
// operator's run it waits for a busy conversation, and like the runner's wake it returns
// when the turn ends. The turn's frames go to the run; the caller sees only its error. A
// wake that cannot be registered -- no registry, or a full one -- runs unobserved.
func (s *Server) WakeWithSteer(ctx context.Context, conv string, pusher runner.SteerPusher, source, text string) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		waker, ok := s.run.(steerWakeRunner)
		if !ok {
			yield(nil, errors.New("steer wake: runner cannot wake a conversation"))
			return
		}
		if s.runs == nil {
			forward(waker.WakeWithSteer(ctx, conv, pusher, source, text), yield)
			return
		}
		owner := identityctx.IdentityID(ctx)
		ctx, err := s.wakeRoute(ctx, owner, conv)
		if err != nil {
			yield(nil, fmt.Errorf("steer wake: %w", err))
			return
		}
		unlock, err := waker.LockThread(ctx, conv)
		if err != nil {
			yield(nil, err)
			return
		}
		ctx = runner.WithThreadLockHeld(ctx)
		dctx, cancel := context.WithTimeout(ctx, s.runs.cfg.maxWallclock)
		sess, err := s.runs.Start(runParams{
			runID: "run-" + uuid.NewString(), threadID: conv, identityID: owner,
			cancel: cancel, unlock: unlock,
		})
		if err != nil {
			cancel()
			defer unlock()
			slog.Warn("agui: steer wake runs unobserved", "conversation", conv, "source", source, "err", err)
			forward(waker.WakeWithSteer(ctx, conv, pusher, source, text), yield)
			return
		}
		var failure error
		turn := func(yieldTurn func(*agent.Event, error) bool) {
			for ev, err := range waker.WakeWithSteer(dctx, conv, pusher, source, text) {
				if err != nil {
					failure = err
				}
				if !yieldTurn(ev, err) {
					return
				}
			}
		}
		s.runProducer(dctx, cancel, sess, Translate(conv, sess.RunID, s.idgen, turn, true))
		if failure != nil {
			yield(nil, failure)
		}
	}
}

// wakeRoute refuses a host-only wake whose owner or conversation is not one, before anything
// is locked or consumed, and scopes ctx to the owner.
func (s *Server) wakeRoute(ctx context.Context, owner, conv string) (context.Context, error) {
	if _, err := uuid.Parse(owner); err != nil {
		return nil, fmt.Errorf("wake owner: %w", err)
	}
	if _, err := uuid.Parse(conv); err != nil {
		return nil, fmt.Errorf("wake conversation: %w", err)
	}
	ctx = identityctx.WithIdentityID(ctx, owner)
	if _, err := s.conv.GetForIdentity(ctx, conv, owner); err != nil {
		return nil, err
	}
	return ctx, nil
}

func forward(seq iter.Seq2[*agent.Event, error], yield func(*agent.Event, error) bool) {
	for ev, err := range seq {
		if !yield(ev, err) {
			return
		}
	}
}
