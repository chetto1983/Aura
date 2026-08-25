package runner

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/askuser"
)

const expiredApprovalContent = "approval expired before a decision was made"

// ExpirePendingApprovals resolves due approval pauses without driving a model turn.
// Each candidate uses the same ResumeCommitter transaction as a human decision, so
// the conditional claim and its wire-valid RoleTool answer commit together.
func (r *Runner) ExpirePendingApprovals(ctx context.Context, cutoff time.Time, limit int) (int, error) {
	if r.approvalExpiry == nil {
		return 0, fmt.Errorf("expire pending approvals: expiry store is not configured")
	}
	due, err := r.approvalExpiry.ListExpiredPendingApprovals(ctx, cutoff, limit)
	if err != nil {
		return 0, fmt.Errorf("expire pending approvals: %w", err)
	}
	expired := 0
	for _, pending := range due {
		resp := ResponseInput{Action: askuser.ActionExpired, Content: expiredApprovalContent}
		claim := r.resumeClaim(pending.Token, pending, resp)
		if err := r.resumeCommitter.CommitResume(ctx, claim); err != nil {
			if errors.Is(err, askuser.ErrPauseNotFound) {
				continue
			}
			return expired, fmt.Errorf("expire pending approval %s: %w", pending.Token, err)
		}
		expired++
		if err := r.applyResumeHook(ctx, pending, resp); err != nil {
			return expired, fmt.Errorf("expire pending approval %s hook: %w", pending.Token, err)
		}
	}
	return expired, nil
}
