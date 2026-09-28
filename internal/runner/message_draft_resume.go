package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/messagedrafts"
)

// MessageDraftResolution returns only a disposition and continuation decision.
// The API reads the owner's review projection separately from the draft store.
type MessageDraftResolution struct {
	Status    messagedrafts.Status
	Directive ResolveDirective
}

// ListMessageDrafts joins draft rows to durable pending pauses. A failed pause
// commit can leave an orphan draft; it is never shown as actionable. Terminal
// drafts with an unresolved answer remain visible for safe completion on reload.
func (r *Runner) ListMessageDrafts(ctx context.Context, conversationID string) ([]messagedrafts.Draft, error) {
	ownerID := identityctx.IdentityID(ctx)
	if ownerID == "" || r.messageDrafts == nil || r.pause == nil {
		return nil, messagedrafts.ErrUnavailable
	}
	pendings, err := r.pause.ListPending(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	active, err := r.messageDrafts.ListPending(ctx, ownerID, conversationID)
	if err != nil {
		return nil, err
	}
	activeByID := make(map[string]messagedrafts.Draft, len(active))
	for _, draft := range active {
		activeByID[draft.ID] = draft
	}
	drafts := make([]messagedrafts.Draft, 0)
	for _, pending := range pendings {
		id, ok := messagedrafts.PauseDraftID(pending.ResumeContext)
		if !ok {
			continue
		}
		draft, found := activeByID[id]
		if !found {
			var getErr error
			draft, getErr = r.messageDrafts.Reconcile(ctx, ownerID, id)
			if errors.Is(getErr, messagedrafts.ErrUnavailable) {
				continue
			}
			if getErr != nil {
				return nil, getErr
			}
		}
		if draft.ConversationID == conversationID && draft.ToolCallID == pending.ToolCallID {
			drafts = append(drafts, draft)
		}
	}
	return drafts, nil
}

// ResolveMessageDraft consumes one owner-scoped review. A terminal draft whose
// answer commit failed may be finalized again, but no terminal draft is dispatched
// again. The message draft CAS is the one-send boundary.
func (r *Runner) ResolveMessageDraft(ctx context.Context, draftID, action string, overrides json.RawMessage) (MessageDraftResolution, error) {
	if r.messageDrafts == nil || r.pause == nil || r.resumeCommitter == nil {
		return MessageDraftResolution{}, messagedrafts.ErrUnavailable
	}
	ownerID := identityctx.IdentityID(ctx)
	if ownerID == "" || (action != "send" && action != "decline") {
		return MessageDraftResolution{}, messagedrafts.ErrUnavailable
	}
	draft, err := r.messageDrafts.Get(ctx, ownerID, draftID)
	if err != nil {
		return MessageDraftResolution{}, err
	}
	if !threadLockHeld(ctx) {
		unlock, ok := r.TryLockThread(ctx, draft.ConversationID)
		if !ok {
			return MessageDraftResolution{}, ErrThreadBusy
		}
		defer unlock()
	}
	draft, err = r.messageDrafts.Reconcile(ctx, ownerID, draftID)
	if err != nil {
		return MessageDraftResolution{}, err
	}
	pending, err := r.pendingForMessageDraft(ctx, draft)
	if err != nil {
		return MessageDraftResolution{}, err
	}

	content := ""
	switch action {
	case "decline":
		if draft.Status == messagedrafts.StatusPending {
			draft, err = r.messageDrafts.Decline(ctx, ownerID, draft.ID)
			if err != nil {
				return MessageDraftResolution{}, err
			}
		}
		if draft.Status != messagedrafts.StatusDeclined && draft.Status != messagedrafts.StatusExpired {
			return MessageDraftResolution{}, messagedrafts.ErrUnavailable
		}
		if draft.Status == messagedrafts.StatusExpired {
			content = "message review expired without sending"
		} else {
			content = "user declined to send the message"
		}
	case "send":
		if draft.Status == messagedrafts.StatusPending {
			if r.registry == nil {
				return MessageDraftResolution{}, messagedrafts.ErrUnavailable
			}
			tool, ok := r.registry.Get(draft.RegisteredToolName)
			if !ok {
				return MessageDraftResolution{}, messagedrafts.ErrUnavailable
			}
			spec := tool.Spec()
			target, trusted := messagedrafts.TrustedOutboundTarget(spec, draft.OriginalArgs)
			if !trusted || target != draft.Target || spec.Name != draft.RegisteredToolName {
				return MessageDraftResolution{}, messagedrafts.ErrUnavailable
			}
			draft, err = r.messageDrafts.ClaimSend(ctx, ownerID, draft.ID, overrides)
			if err != nil {
				return MessageDraftResolution{}, err
			}
			result, executeErr := agent.ExecuteReviewedMessage(ctx, tool, r.gateway, draft, r.runDir, r.previewCap)
			status, code := reviewedMessageOutcome(executeErr)
			// The HTTP request can disappear after the sidecar has sent. Preserve the
			// disposition on a bounded detached context so retry never dispatches.
			outcomeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			draft, err = r.messageDrafts.MarkOutcome(outcomeCtx, ownerID, draft.ID, status, code)
			cancel()
			if err != nil {
				return MessageDraftResolution{}, fmt.Errorf("record message dispatch outcome: %w", err)
			}
			if executeErr == nil {
				content = result.Preview
			}
		}
		if draft.Status != messagedrafts.StatusSent && draft.Status != messagedrafts.StatusFailed && draft.Status != messagedrafts.StatusUncertain {
			return MessageDraftResolution{}, messagedrafts.ErrUnavailable
		}
		if content == "" {
			content = reviewedMessageAnswer(draft.Status)
		}
	}

	resumeAction := askuser.ActionAccept
	if action == "decline" {
		resumeAction = askuser.ActionDecline
	}
	claim := r.resumeClaim(pending.Token, pending, ResponseInput{Action: resumeAction, Content: content})
	// The ordinary decline marker refers to answering a question. This pause was
	// a send request, so the tool result must state that no message was sent.
	claim.Turn.Content = content
	claim.Answer.Content = content
	if err := r.resumeCommitter.CommitResume(ctx, claim); err != nil {
		return MessageDraftResolution{}, fmt.Errorf("commit message review answer: %w", err)
	}
	remaining, err := r.remainingPending(ctx, draft.ConversationID)
	if err != nil {
		return MessageDraftResolution{}, err
	}
	return MessageDraftResolution{Status: draft.Status, Directive: classifyResolve(pending, resumeAction, remaining)}, nil
}

func (r *Runner) pendingForMessageDraft(ctx context.Context, draft messagedrafts.Draft) (askuser.Pending, error) {
	pendings, err := r.pause.ListPending(ctx, draft.ConversationID)
	if err != nil {
		return askuser.Pending{}, err
	}
	for _, pending := range pendings {
		id, ok := messagedrafts.PauseDraftID(pending.ResumeContext)
		if ok && id == draft.ID && pending.ToolCallID == draft.ToolCallID && pending.ConversationID == draft.ConversationID {
			return pending, nil
		}
	}
	return askuser.Pending{}, messagedrafts.ErrUnavailable
}

func reviewedMessageOutcome(err error) (messagedrafts.Status, string) {
	if err == nil {
		return messagedrafts.StatusSent, "ok"
	}
	var callErr *mcp.ToolCallError
	if errors.As(err, &callErr) && callErr.DeterministicNoEffect() {
		return messagedrafts.StatusFailed, "no_effect"
	}
	return messagedrafts.StatusUncertain, "unknown_effect"
}

func reviewedMessageAnswer(status messagedrafts.Status) string {
	switch status {
	case messagedrafts.StatusSent:
		return "message sent"
	case messagedrafts.StatusFailed:
		return "message was not sent"
	default:
		return "message delivery is uncertain; do not retry this call automatically"
	}
}
