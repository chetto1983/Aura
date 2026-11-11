package cron

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
)

// ActivateReviewedMessage activates the owner's message task its draft review approved. Only
// the review does this (spec 2026-11-05 Part 2); an already active task succeeds again, so a
// retried review commit is safe.
func (s *Store) ActivateReviewedMessage(ctx context.Context, ownerID, taskID string) error {
	return s.settleReviewedMessage(ctx, ownerID, taskID, s.q.ActivateReviewedMessageTask)
}

// CancelReviewedMessage cancels the owner's message task its review declined.
func (s *Store) CancelReviewedMessage(ctx context.Context, ownerID, taskID string) error {
	return s.settleReviewedMessage(ctx, ownerID, taskID, func(ctx context.Context, p sqlc.ActivateReviewedMessageTaskParams) (int64, error) {
		return s.q.CancelReviewedMessageTask(ctx, sqlc.CancelReviewedMessageTaskParams(p))
	})
}

func (s *Store) settleReviewedMessage(ctx context.Context, ownerID, taskID string,
	update func(context.Context, sqlc.ActivateReviewedMessageTaskParams) (int64, error),
) error {
	id, err := db.ParseUUID("task id", taskID)
	if err != nil {
		return err
	}
	n, err := update(ctx, sqlc.ActivateReviewedMessageTaskParams{ID: id, IdentityID: ownerID})
	if err != nil {
		return fmt.Errorf("message task %s: %w", taskID, err)
	}
	if n == 0 {
		return fmt.Errorf("message task %s is not awaiting its review", taskID)
	}
	return nil
}
