package handlers

// message.go sends one reviewed message to someone else at its time, with no model in between
// (docs/superpowers/specs/2026-11-05-scheduled-delivery-defaults-and-messages-design.md, Part 2).
// The draft's approved -> dispatching claim is the one-send boundary: a second fire, a boot
// catch-up or a retry finds the draft claimed and sends nothing.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/gateway"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/messagedrafts"
)

// messageMaxDuration bounds one send: a tool call and two short writes.
const messageMaxDuration = 2 * time.Minute

// staleDispatch is how long a send may be in flight before its outcome is called uncertain,
// as Reconcile does for an immediate draft.
const staleDispatch = 30 * time.Minute

// MessageDrafts is the drafts seam the handler needs; *messagedrafts.Store satisfies it.
type MessageDrafts interface {
	ForTask(ctx context.Context, ownerID, taskID string) (messagedrafts.Draft, error)
	ClaimScheduled(ctx context.Context, ownerID, draftID string) (messagedrafts.Draft, error)
	ExpireScheduled(ctx context.Context, ownerID, draftID string) (messagedrafts.Draft, error)
	MarkOutcome(ctx context.Context, ownerID, draftID string, status messagedrafts.Status, code string) (messagedrafts.Draft, error)
}

// MessageHandler sends a message task's reviewed draft through the trusted tool it was reviewed
// against, under the gateway, and reports the outcome on the task's notify route.
type MessageHandler struct {
	Drafts     MessageDrafts
	Tools      *tools.Registry
	Gateway    *gateway.Gateway
	RunDir     string
	PreviewCap int
	Timezones  jobTimezones
	Location   *time.Location
}

// Meta reschedules a message on recovery, so a fire missed while the daemon was down is sent
// when it comes back, inside its review.
func (h MessageHandler) Meta() HandlerMeta {
	return HandlerMeta{Kind: KindMessage, MaxDuration: messageMaxDuration, ReschedulesOnRecovery: true}
}

// Run sends the task's approved draft once, or reports why it was not sent.
func (h MessageHandler) Run(ctx context.Context, job Job) (string, error) {
	owner := identityctx.IdentityID(ctx)
	draft, err := h.Drafts.ForTask(ctx, owner, job.TaskID)
	if err != nil {
		return "", fmt.Errorf("scheduled message: no reviewed draft for task %s", job.TaskID)
	}
	if draft.Status == messagedrafts.StatusApproved {
		return h.send(ctx, owner, job, draft)
	}
	return h.settle(ctx, owner, draft)
}

// send claims the approved draft and runs the trusted tool once. A lost claim reads the draft
// again, so a cancel that won the race reads declined, not expired (decision 39).
func (h MessageHandler) send(ctx context.Context, owner string, job Job, draft messagedrafts.Draft) (string, error) {
	recipient, _, _ := draft.Review()
	tool, ok := h.trustedTool(draft)
	if !ok {
		return "", fmt.Errorf("scheduled message to %s not sent: its channel is not connected", recipient)
	}
	claim, err := h.Drafts.ClaimScheduled(ctx, owner, draft.ID)
	if errors.Is(err, messagedrafts.ErrUnavailable) {
		current, readErr := h.Drafts.ForTask(ctx, owner, job.TaskID)
		if readErr != nil {
			return "", fmt.Errorf("scheduled message: no reviewed draft for task %s", job.TaskID)
		}
		return h.settle(ctx, owner, current)
	}
	if err != nil {
		return "", fmt.Errorf("scheduled message to %s: claim: %w", recipient, err)
	}
	_, sendErr := agent.ExecuteReviewedMessage(ctx, tool, h.Gateway, claim, h.RunDir, h.PreviewCap)
	status, code := agent.ReviewedMessageOutcome(sendErr)
	outcomeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	_, markErr := h.Drafts.MarkOutcome(outcomeCtx, owner, claim.ID, status, code)
	cancel()
	if markErr != nil {
		return "", fmt.Errorf("scheduled message to %s: record its outcome: %w", recipient, markErr)
	}
	switch status {
	case messagedrafts.StatusSent:
		return fmt.Sprintf("Message sent to %s at %s.", recipient, time.Now().In(h.location(ctx)).Format("15:04")), nil
	case messagedrafts.StatusFailed:
		return "", fmt.Errorf("message to %s not sent: the channel reported it did not go out", recipient)
	default:
		return "", fmt.Errorf("message to %s: delivery is uncertain, and it was not retried", recipient)
	}
}

// settle reports a draft that is not approved: it was never sent, or it was already handled.
func (h MessageHandler) settle(ctx context.Context, owner string, draft messagedrafts.Draft) (string, error) {
	recipient, _, _ := draft.Review()
	switch draft.Status {
	case messagedrafts.StatusPending:
		// Only the review activates a message task, so a pending draft never reaches here; if one
		// does, it is closed rather than sent.
		if _, err := h.Drafts.ExpireScheduled(ctx, owner, draft.ID); err != nil && !errors.Is(err, messagedrafts.ErrUnavailable) {
			return "", fmt.Errorf("scheduled message to %s: expire: %w", recipient, err)
		}
		return fmt.Sprintf("Scheduled message to %s not sent: it was not approved in time.", recipient), nil
	case messagedrafts.StatusApproved:
		// An approved draft whose claim was refused is past its review.
		if _, err := h.Drafts.ExpireScheduled(ctx, owner, draft.ID); err != nil && !errors.Is(err, messagedrafts.ErrUnavailable) {
			return "", fmt.Errorf("scheduled message to %s: expire: %w", recipient, err)
		}
		return fmt.Sprintf("Scheduled message to %s not sent: it expired before it could be sent.", recipient), nil
	case messagedrafts.StatusDeclined:
		return fmt.Sprintf("Scheduled message to %s not sent: it was declined.", recipient), nil
	case messagedrafts.StatusExpired:
		return fmt.Sprintf("Scheduled message to %s not sent: it expired before it could be sent.", recipient), nil
	case messagedrafts.StatusDispatching:
		if !draft.DispatchStartedAt.IsZero() && time.Since(draft.DispatchStartedAt) > staleDispatch {
			if _, err := h.Drafts.MarkOutcome(ctx, owner, draft.ID, messagedrafts.StatusUncertain, "stale_dispatch"); err != nil {
				return "", fmt.Errorf("scheduled message to %s: record its outcome: %w", recipient, err)
			}
			return "", fmt.Errorf("message to %s: delivery is uncertain, and it was not retried", recipient)
		}
	}
	return fmt.Sprintf("Scheduled message to %s was already handled (%s); nothing was sent again.", recipient, draft.Status), nil
}

// trustedTool is the tool the draft was reviewed against, checked as the cockpit's resolve
// checks it: still mounted under that name, from the same recipe.
func (h MessageHandler) trustedTool(draft messagedrafts.Draft) (tools.Tool, bool) {
	if h.Tools == nil {
		return nil, false
	}
	tool, ok := h.Tools.Get(draft.RegisteredToolName)
	if !ok {
		return nil, false
	}
	spec := tool.Spec()
	target, trusted := messagedrafts.TrustedOutboundTarget(spec, draft.OriginalArgs)
	if !trusted || target != draft.Target || spec.Name != draft.RegisteredToolName {
		return nil, false
	}
	return tool, true
}

func (h MessageHandler) location(ctx context.Context) *time.Location {
	if loc := jobLocation(ctx, h.Timezones, h.Location); loc != nil {
		return loc
	}
	return time.UTC
}
