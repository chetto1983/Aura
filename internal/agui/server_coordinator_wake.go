package agui

import (
	"context"
	"fmt"
	"iter"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/google/uuid"
)

type pendingSteerRunner interface {
	PreparePendingSteer(context.Context, string) (iter.Seq2[*agent.Event, error], bool, error)
}

// ResumePendingSteer is a host-only continuation entry. Saved report ownership,
// the ordinary thread lock and the shared run cap are checked before consuming it.
func (s *Server) ResumePendingSteer(ctx context.Context, owner, conv string) (bool, error) {
	if s.runs == nil {
		return false, nil
	}
	if _, err := uuid.Parse(owner); err != nil {
		return false, fmt.Errorf("coordinator wake owner: %w", err)
	}
	if _, err := uuid.Parse(conv); err != nil {
		return false, fmt.Errorf("coordinator wake conversation: %w", err)
	}
	ctx = identityctx.WithIdentityID(ctx, owner)
	if _, err := s.conv.GetForIdentity(ctx, conv, owner); err != nil {
		return false, err
	}
	prepared, ok := s.run.(pendingSteerRunner)
	if !ok {
		return false, fmt.Errorf("coordinator wake: runner cannot consume pending steer")
	}
	locker, ok := s.run.(threadTryLocker)
	if !ok {
		return false, fmt.Errorf("coordinator wake: runner has no conversation lock")
	}
	unlock, locked := locker.TryLockThread(ctx, conv)
	if !locked {
		return false, nil
	}
	ctx = runner.WithThreadLockHeld(ctx)
	dctx, cancel := context.WithTimeout(ctx, s.runs.cfg.maxWallclock)
	sess, err := s.runs.Start(runParams{
		runID: "run-" + uuid.NewString(), threadID: conv, identityID: owner,
		cancel: cancel, unlock: unlock,
	})
	if err != nil {
		cancel()
		unlock()
		return false, err
	}
	turn, ready, err := prepared.PreparePendingSteer(dctx, conv)
	if err != nil || !ready {
		cancel()
		sess.finish()
		return false, err
	}
	go s.runProducer(dctx, cancel, sess, Translate(conv, sess.RunID, s.idgen, turn, true))
	return true, nil
}
