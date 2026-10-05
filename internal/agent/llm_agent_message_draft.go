package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/gateway"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/messagedrafts"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// MessageDraftCreator is the only persistence operation the agent needs before
// it parks a trusted outbound call. The Runner supplies the process-wide store.
type MessageDraftCreator interface {
	Create(context.Context, messagedrafts.DraftInput) (messagedrafts.Draft, error)
}

func outboundMessageTarget(spec tools.Spec, raw json.RawMessage) (messagedrafts.Target, bool) {
	return messagedrafts.TrustedOutboundTarget(spec, raw)
}

func (a *LlmAgent) withholdOutboundMessage(ctx context.Context, call llm.ToolCall) (pauseCall, bool) {
	tool, ok := a.registry.Get(call.Function.Name)
	if !ok {
		return pauseCall{}, false
	}
	spec := tool.Spec()
	target, outbound := outboundMessageTarget(spec, json.RawMessage(call.Function.Arguments))
	if !outbound || a.messageDrafts == nil || !gateway.HasResponder(ctx) {
		return pauseCall{}, false
	}
	ownerID := identityctx.IdentityID(ctx)
	if ownerID == "" {
		return pauseCall{}, false
	}
	draft, err := a.messageDrafts.Create(ctx, messagedrafts.DraftInput{
		IdentityID: ownerID, ConversationID: a.ledgerConvID, ToolCallID: call.ID,
		Target: target, RegisteredToolName: spec.Name,
		OriginalArgs: json.RawMessage(call.Function.Arguments), ExpiresAt: time.Now().Add(30 * time.Minute),
	})
	if err != nil {
		slog.Error("agent: could not persist outbound message draft", "error", safeMessageDraftError(err))
		return pauseCall{}, false // execTool's outbound guard denies; it cannot dispatch
	}
	contextJSON, err := json.Marshal(map[string]string{"type": "message_draft", "draft_id": draft.ID})
	if err != nil {
		return pauseCall{}, false
	}
	return pauseCall{
		call: call,
		pause: &tools.ErrAwaitingUserInput{
			Question:      "Review this message before sending",
			Kind:          "approval",
			Priority:      90,
			ToolCallID:    call.ID,
			ResumeContext: contextJSON,
		},
		originalToolName: spec.Name, originalArguments: call.Function.Arguments,
	}, true
}

func safeMessageDraftError(err error) string {
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, context.Canceled):
		return "context canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "context deadline exceeded"
	case errors.Is(err, messagedrafts.ErrDuplicate):
		return "duplicate tool call"
	case errors.Is(err, pgx.ErrNoRows):
		return "database row unavailable"
	case errors.As(err, &pgErr):
		return "postgres SQLSTATE " + pgErr.Code
	default:
		return fmt.Sprintf("error type %T", err)
	}
}

// ExecuteReviewedMessage re-enters the normal gateway PEP with the durable
// one-send claim and the exact tool call that was parked before transport.
//
// It runs outside any model loop, under the resolve endpoint's HTTP operation, so the
// dispatch is its own single model round: deriveToolOperationContext refuses to derive a
// child operation without one. Before the round was declared here, every approved send
// failed there, ahead of the gateway and the transport (measured on 2026-10-05).
func ExecuteReviewedMessage(ctx context.Context, tool tools.Tool, policy *gateway.Gateway, claim messagedrafts.Draft, runDir string, previewCap int) (tools.ToolResult, error) {
	ctx = tools.WithRequestID(ctx, claim.ID)
	ctx = tools.WithToolCallContext(ctx, claim.ConversationID, claim.ToolCallID, runDir, previewCap)
	ctx = gateway.WithReviewedMessageDraft(ctx, claim)
	ctx = withModelRound(ctx, modelRound{ordinal: 1})
	a := &LlmAgent{gateway: policy, ledgerConvID: claim.ConversationID}
	return a.execTool(ctx, tool, true, claim.EffectiveArgs)
}
