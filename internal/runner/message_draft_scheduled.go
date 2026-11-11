package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/messagedrafts"
)

// ScheduledMessageTasks settles the message task a scheduled draft's review answers. Only that
// review activates a message task (spec 2026-11-05 Part 2); cron.Store implements it.
type ScheduledMessageTasks interface {
	ActivateReviewedMessage(ctx context.Context, ownerID, taskID string) error
	CancelReviewedMessage(ctx context.Context, ownerID, taskID string) error
}

// resolveScheduledMessage answers the review of a draft a task sends at its time. Nothing is
// sent here: send approves the draft and activates its task, decline declines both. It returns
// the draft and the tool answer the review commits.
//
// An approval whose activation fails returns the error with the draft left approved, so the
// card's continue retries the activation and nothing else.
func (r *Runner) resolveScheduledMessage(ctx context.Context, ownerID string, draft messagedrafts.Draft, action string, overrides json.RawMessage) (messagedrafts.Draft, string, error) {
	if r.messageTasks == nil {
		return draft, "", messagedrafts.ErrUnavailable
	}
	var err error
	switch {
	case draft.Status == messagedrafts.StatusExpired:
		return draft, "message review expired without sending", nil
	case action == "decline" && draft.Status == messagedrafts.StatusPending:
		if draft, err = r.messageDrafts.Decline(ctx, ownerID, draft.ID); err != nil {
			return draft, "", err
		}
		if err := r.messageTasks.CancelReviewedMessage(ctx, ownerID, draft.ScheduledTaskID); err != nil {
			return draft, "", err
		}
		return draft, fmt.Sprintf("the operator declined the scheduled message; nothing will be sent and task %s is cancelled", draft.ScheduledTaskID), nil
	case action == "send" && (draft.Status == messagedrafts.StatusPending || draft.Status == messagedrafts.StatusApproved):
		if draft.Status == messagedrafts.StatusPending {
			if draft, err = r.messageDrafts.Approve(ctx, ownerID, draft.ID, overrides); err != nil {
				return draft, "", err
			}
		}
		if err := r.messageTasks.ActivateReviewedMessage(ctx, ownerID, draft.ScheduledTaskID); err != nil {
			return draft, "", err
		}
		return draft, fmt.Sprintf("scheduled message approved: it will be sent at %s, and its outcome reported on the task's notify route (task %s)",
			draft.SendAt.UTC().Format(time.RFC3339), draft.ScheduledTaskID), nil
	default:
		return draft, "", messagedrafts.ErrUnavailable
	}
}
