package pimwatch

import (
	"context"
	"strings"

	"github.com/chetto1983/aura/internal/cron"
)

// cursorReader is the part of *Store that Repaired reads.
type cursorReader interface {
	Cursors(ctx context.Context, identityID string) ([]Cursor, error)
}

// Repaired wakes an account's watch after the account was repaired. A watch the scheduler paused
// after repeated failures of a kind a repair cures (provider_auth, provider_transient or a pim_*
// code) resumes and runs now. An active watch runs now. A watch paused for another cause, such as
// the operator or a refused permission, stays paused, and an account with no watch has nothing to
// wake. A repair landing while a run is in flight takes effect at the next interval: the
// scheduler's skip path reschedules.
func (s *Service) Repaired(ctx context.Context, identityID, accountID string) error {
	watches, err := s.Tasks.Watches(ctx, identityID)
	if err != nil {
		return err
	}
	watch, ok := findWatch(watches, accountID)
	if !ok {
		return nil
	}
	if !watch.Paused {
		return s.Tasks.RunNow(ctx, watch.ID)
	}
	if watch.PausedReason != cron.PausedByFailures {
		return nil
	}
	cursors, err := s.cursorSource().Cursors(ctx, identityID)
	if err != nil {
		return err
	}
	if !failedForACredential(cursors, accountID) {
		return nil
	}
	if err := s.Tasks.Resume(ctx, watch.ID); err != nil {
		return err
	}
	return s.Tasks.RunNow(ctx, watch.ID)
}

func (s *Service) cursorSource() cursorReader {
	if s.cursors != nil {
		return s.cursors
	}
	return s.Store
}

func failedForACredential(cursors []Cursor, accountID string) bool {
	for _, c := range cursors {
		if c.AccountID != accountID {
			continue
		}
		if c.LastError == "provider_auth" || c.LastError == "provider_transient" || strings.HasPrefix(c.LastError, "pim_") {
			return true
		}
	}
	return false
}
