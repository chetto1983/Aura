package pimwatch

import (
	"context"
	"strings"

	"github.com/chetto1983/aura/internal/cron"
)

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
	// Only a paused watch needs to know why it failed; a cursor read that fails must not keep an
	// active watch from running.
	var cursors []Cursor
	if watch.Paused {
		if cursors, err = s.Store.Cursors(ctx, identityID); err != nil {
			return err
		}
	}
	resume, runNow := repairSteps(watch, cursors, accountID)
	if resume {
		if err := s.Tasks.Resume(ctx, watch.ID); err != nil {
			return err
		}
	}
	if runNow {
		return s.Tasks.RunNow(ctx, watch.ID)
	}
	return nil
}

// repairSteps says what a repair does to the watch: resume it, run it now, both or neither.
func repairSteps(watch WatchTask, cursors []Cursor, accountID string) (resume, runNow bool) {
	if !watch.Paused {
		return false, true
	}
	if watch.PausedReason != cron.PausedByFailures || !failedForACredential(cursors, accountID) {
		return false, false
	}
	return true, true
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
