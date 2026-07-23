package runner

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// approvalPauseKind is the pause Kind every approval carries ("approval"), the value the
// channels render as a Sì/No / accept-decline prompt (mirrors tools.KindApproval without an
// internal/agent/tools import here).
const approvalPauseKind = "approval"

// MintApprovalPause creates a WIRE-VALID operator-origin approval pause on convID: it writes
// the synthetic assistant ask_user tool_call turn AND the paused_states row in ONE CommitPause
// tx — the same primitive flushPause uses for a model-relayed pause (runner_persist.go). The
// minted pause is therefore indistinguishable from a real ask_user pause, so its later RoleTool
// answer (on accept/decline) references a real assistant tool_call and never leaves an orphan
// tool message that would break the rehydrated history on the next Turn.
//
// It is the deterministic host-side creator the scheduler approval-reminder sweep uses when the
// model never relayed the approval directive (Amendment #92 revised / fix-plan 1.7): the sweep
// ensures a pause exists on the task's origin conversation so the approval surfaces as a real
// HITL prompt on the origin channel (Telegram Sì/No, WebUI in-thread) regardless of the model.
//
// Kind is always "approval". resumeContext is the caller's machine-readable payload the
// ResumeHook decodes (e.g. {"type":"scheduled_task_approval","task_id":<id>}). Returns the
// pause token the caller pushes to the channel and the operator resolves.
func (r *Runner) MintApprovalPause(ctx context.Context, convID, question string, resumeContext json.RawMessage) (string, error) {
	token, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("mint approval pause: token: %w", err)
	}
	toolCallID, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("mint approval pause: tool_call id: %w", err)
	}
	// Reuse the canonical assistant-turn builder so the synthetic pause is byte-shaped exactly
	// like a model-relayed one (D-A1-07 wire-correctness): ONE assistant ask_user tool_call
	// whose id the pause's ToolCallID references.
	ai := &agent.AwaitingInput{
		Question:      question,
		Kind:          approvalPauseKind,
		ToolCallID:    toolCallID.String(),
		ResumeContext: resumeContext,
	}
	toolCalls, err := assistantAskUserToolCalls([]*agent.AwaitingInput{ai})
	if err != nil {
		return "", fmt.Errorf("mint approval pause: %w", err)
	}
	assistantTurn := conversations.AppendTurnParams{
		ConversationID: convID,
		Role:           llm.RoleAssistant,
		ToolCalls:      toolCalls,
	}
	pause := askuser.InsertParams{
		Token:          token.String(),
		ConversationID: convID,
		Kind:           approvalPauseKind,
		Question:       question,
		ToolCallID:     ai.ToolCallID,
		ResumeContext:  resumeContext,
	}
	if err := r.resumeCommitter.CommitPause(ctx, assistantTurn, []askuser.InsertParams{pause}); err != nil {
		return "", fmt.Errorf("mint approval pause: %w", err)
	}
	return token.String(), nil
}
