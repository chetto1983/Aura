package cronadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aura/aura/internal/chat"
	"github.com/aura/aura/internal/cron"
	"github.com/aura/aura/internal/identity"
)

// CronAgentLoop implements chat.AgentLoop for KindAgentJob tasks arriving via
// the cron Hub. It delegates execution to a cron.JobRunner and emits the
// result as OutboundEvents so the silent Outbound can log them.
type CronAgentLoop struct {
	runner   cron.JobRunner
	loc      *time.Location
	identity identity.Delegator
}

type CronAgentLoopOption func(*CronAgentLoop)

func WithIdentity(delegator identity.Delegator) CronAgentLoopOption {
	return func(l *CronAgentLoop) {
		l.identity = delegator
	}
}

// NewCronAgentLoop constructs a CronAgentLoop. A nil loc falls back to time.Local.
func NewCronAgentLoop(runner cron.JobRunner, loc *time.Location, opts ...CronAgentLoopOption) *CronAgentLoop {
	if loc == nil {
		loc = time.Local
	}
	loop := &CronAgentLoop{runner: runner, loc: loc}
	for _, opt := range opts {
		if opt != nil {
			opt(loop)
		}
	}
	return loop
}

// Run satisfies chat.AgentLoop.
func (l *CronAgentLoop) Run(ctx context.Context, run *chat.Run, msg chat.InboundMessage, emit chat.EmitFn) error {
	payload, err := cron.NormalizeAgentJobPayload(msg.Text)
	if err != nil {
		return fmt.Errorf("cron agent loop: parse payload: %w", err)
	}
	allowlist := cron.SafeAgentJobTools(payload.ToolAllowlist)
	runCtx, err := l.delegateCronActor(ctx, run, msg, allowlist)
	if err != nil {
		return err
	}
	zero := 0.0
	result, err := l.runner.RunJob(runCtx, cron.JobRequest{
		RunID:         identity.RunIDFromContext(runCtx),
		SystemPrompt:  cron.AgentJobSystemPrompt(payload, time.Now(), l.loc),
		Prompt:        cron.AgentJobUserPrompt(payload),
		ToolAllowlist: allowlist,
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

func (l *CronAgentLoop) delegateCronActor(ctx context.Context, run *chat.Run, msg chat.InboundMessage, allowlist []string) (context.Context, error) {
	delegator, ok := identity.DelegatorFromContext(ctx)
	if !ok {
		delegator = l.identity
	}
	if delegator == nil {
		return ctx, fmt.Errorf("%w: cron agent loop requires identity delegator", identity.ErrUnauthorized)
	}
	parentActorID := identity.ActorIDFromContext(ctx)
	if parentActorID == "" && msg.PrincipalID != "" && msg.PrincipalID != "cron" {
		parentActorID = identity.TelegramSessionActorID(msg.PrincipalID)
	}
	if parentActorID == "" {
		return ctx, fmt.Errorf("%w: cron agent loop requires parent actor", identity.ErrUnauthorized)
	}
	scopeID := "cron:" + msg.ThreadID
	if run != nil && run.ID != "" {
		scopeID = run.ID
	}
	result, err := delegator.DelegateActor(ctx, identity.DelegateActorParams{
		ID:              identity.DelegatedActorID(identity.ActorTypeCron, parentActorID, scopeID),
		ParentActorID:   parentActorID,
		ActorType:       identity.ActorTypeCron,
		RunID:           scopeID,
		Capabilities:    delegatedCronCapabilities(allowlist),
		ConstraintsJSON: cronDelegationConstraints(msg, allowlist),
	})
	if err != nil {
		return ctx, fmt.Errorf("cron agent loop: delegate actor: %w", err)
	}
	return identity.WithRunID(identity.WithActorID(identity.WithAuthority(ctx, delegator), result.Actor.ID), scopeID), nil
}

func delegatedCronCapabilities(allowlist []string) []identity.Capability {
	capabilities := []identity.Capability{identity.CapabilityCronRun}
	if len(allowlist) > 0 {
		capabilities = append(capabilities, identity.CapabilityToolExecute)
	}
	return capabilities
}

func cronDelegationConstraints(msg chat.InboundMessage, allowlist []string) string {
	data, err := json.Marshal(map[string]any{
		"channel":        "cron",
		"thread_id":      msg.ThreadID,
		"tool_allowlist": allowlist,
	})
	if err != nil {
		return "{}"
	}
	return string(data)
}
