package messagedrafts

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
)

// Scheduled reports whether a task sends this draft at its time (migration 0155).
func (d Draft) Scheduled() bool { return !d.SendAt.IsZero() }

// Approve fixes the effective arguments of a pending scheduled draft before its send time:
// pending -> approved. The task sends exactly these at its time; an immediate draft is never
// approved, only claimed.
func (s *Store) Approve(ctx context.Context, ownerID, draftID string, overrides json.RawMessage) (Draft, error) {
	owner, id, err := draftKey(ownerID, draftID)
	if err != nil {
		return Draft{}, ErrUnavailable
	}
	var approved Draft
	err = db.WithIdentityTx(ctx, s.pool, ownerID, func(q *sqlc.Queries) error {
		row, readErr := q.GetMessageDraftForIdentity(ctx, sqlc.GetMessageDraftForIdentityParams{IdentityID: owner, ID: id})
		if errors.Is(readErr, pgx.ErrNoRows) {
			return ErrUnavailable
		}
		if readErr != nil {
			return readErr
		}
		if row.Status != string(StatusPending) || !row.SendAt.Valid || !row.SendAt.Time.After(time.Now()) {
			return ErrUnavailable
		}
		target := Target{Recipe: row.Recipe, Tool: row.ToolName, Action: row.Action}
		effective, mergeErr := MergeApprovedArgs(target, row.OriginalArgs, overrides)
		if mergeErr != nil {
			return mergeErr
		}
		row, readErr = q.ApproveScheduledMessageDraft(ctx, sqlc.ApproveScheduledMessageDraftParams{
			ID: id, IdentityID: owner, EffectiveArgs: effective,
			EffectiveFingerprint: pgtype.Text{String: argsFingerprint(target, effective), Valid: true},
		})
		if errors.Is(readErr, pgx.ErrNoRows) {
			return ErrUnavailable
		}
		if readErr != nil {
			return readErr
		}
		approved, readErr = draftFromRow(row)
		return readErr
	})
	return approved, err
}

// DeclineForTask declines the task's draft while it is pending or approved; a claimed one is
// untouched. Cancelling a message task calls it before the cancel (decision 15): a handler
// already holding the task then finds its draft declined and claims nothing.
func (s *Store) DeclineForTask(ctx context.Context, ownerID, taskID string) error {
	owner, err := db.ParseUUID("owner id", ownerID)
	if err != nil {
		return ErrUnavailable
	}
	task, err := db.ParseUUID("task id", taskID)
	if err != nil {
		return ErrUnavailable
	}
	return db.WithIdentityTx(ctx, s.pool, ownerID, func(q *sqlc.Queries) error {
		return q.DeclineScheduledMessageDraftsForTask(ctx, sqlc.DeclineScheduledMessageDraftsForTaskParams{
			IdentityID: owner, ScheduledTaskID: task,
		})
	})
}

// DeclineScheduledForOwner declines every pending or approved scheduled draft of the owner, for
// a deactivation (decision 37). It goes by owner, not by task, so a retry still finds them.
func (s *Store) DeclineScheduledForOwner(ctx context.Context, ownerID string) error {
	owner, err := db.ParseUUID("owner id", ownerID)
	if err != nil {
		return ErrUnavailable
	}
	return db.WithIdentityTx(ctx, s.pool, ownerID, func(q *sqlc.Queries) error {
		return q.DeclineScheduledMessageDraftsForOwner(ctx, owner)
	})
}
