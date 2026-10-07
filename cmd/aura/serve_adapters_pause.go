package main

// serve_adapters_pause.go adapts the task tool's pause and resume to internal/cron
// (prd.md §15). Ownership and the kind rule are checked here, inside the identity the tool
// call carries, exactly as CancelScheduledTask does; the status transition itself is the
// cron store's, so the cockpit, the CLI and the agent share one implementation.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/cron"
	"github.com/jackc/pgx/v5"
)

// PauseScheduledTask stops an active task the identity owns from firing until resumed. The
// database backup and system sweeps are never paused, matching the cancel rule.
func (s *cronTaskStore) PauseScheduledTask(ctx context.Context, id string) error {
	if err := s.ownedPausable(ctx, id); err != nil {
		return err
	}
	if err := s.store.PauseTask(ctx, id); err != nil {
		if errors.Is(err, cron.ErrTaskNotFound) {
			return fmt.Errorf("task %s is not active, so it cannot be paused", id)
		}
		return err
	}
	return nil
}

// ResumeScheduledTask reactivates a paused task the identity owns and returns its next fire.
func (s *cronTaskStore) ResumeScheduledTask(ctx context.Context, id string) (time.Time, error) {
	if err := s.ownedPausable(ctx, id); err != nil {
		return time.Time{}, err
	}
	next, err := s.store.ResumeTask(ctx, id, time.Now())
	if errors.Is(err, cron.ErrTaskNotFound) {
		return time.Time{}, fmt.Errorf("task %s is not paused, so it cannot be resumed", id)
	}
	return next, err
}

// ownedPausable confirms the task exists inside the calling identity and is of a kind that
// may be paused, before the cron store is asked to change it.
func (s *cronTaskStore) ownedPausable(ctx context.Context, id string) error {
	identityID, err := taskIdentity(ctx)
	if err != nil {
		return err
	}
	var kind string
	err = s.pool.QueryRow(ctx, `
		SELECT kind FROM aura.scheduler_tasks
		WHERE id = $1::uuid AND identity_id = $2`, id, identityID).Scan(&kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("task %s is not owned by this identity", id)
	}
	if err != nil {
		return fmt.Errorf("task %s: %w", id, err)
	}
	if !cron.IsPausableKind(cron.TaskKind(kind)) {
		return fmt.Errorf("task %s is a %s task, which cannot be paused", id, kind)
	}
	return nil
}
