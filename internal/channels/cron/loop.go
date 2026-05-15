package cronadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/aura/aura/internal/chat"
	"github.com/aura/aura/internal/cron"
)

// CronAgentLoop implements chat.AgentLoop for KindAgentJob tasks arriving via
// the cron Hub. It delegates execution to a cron.JobRunner and emits the
// result as OutboundEvents so the silent Outbound can log them.
type CronAgentLoop struct {
	runner cron.JobRunner
	loc    *time.Location
}

// NewCronAgentLoop constructs a CronAgentLoop. A nil loc falls back to time.Local.
func NewCronAgentLoop(runner cron.JobRunner, loc *time.Location) *CronAgentLoop {
	if loc == nil {
		loc = time.Local
	}
	return &CronAgentLoop{runner: runner, loc: loc}
}

// Run satisfies chat.AgentLoop.
func (l *CronAgentLoop) Run(ctx context.Context, _ *chat.Run, msg chat.InboundMessage, emit chat.EmitFn) error {
	payload, err := cron.NormalizeAgentJobPayload(msg.Text)
	if err != nil {
		return fmt.Errorf("cron agent loop: parse payload: %w", err)
	}
	zero := 0.0
	result, err := l.runner.RunJob(ctx, cron.JobRequest{
		SystemPrompt:  cron.AgentJobSystemPrompt(payload, time.Now(), l.loc),
		Prompt:        cron.AgentJobUserPrompt(payload),
		ToolAllowlist: cron.SafeAgentJobTools(payload.ToolAllowlist),
		UserID:        msg.PrincipalID,
		Temperature:   &zero,
	})
	if err != nil {
		return err
	}
	_ = emit(chat.OutboundEvent{Type: chat.EventMessageDone, Content: result.Content})
	_ = emit(chat.OutboundEvent{
		Type: chat.EventUsage,
		Payload: map[string]any{
			"llm_calls":         result.LLMCalls,
			"tool_calls":        result.ToolCalls,
			"tokens_total":      result.TokensTotal,
			"tokens_prompt":     result.TokensPrompt,
			"tokens_completion": result.TokensCompletion,
		},
	})
	return nil
}
