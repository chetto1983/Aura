package telegram

import (
	"context"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/identity"
	"github.com/aura/aura/internal/llm"

	tele "gopkg.in/telebot.v4"
)

func ChatIDFromTeleContext(c tele.Context) int64 {
	if c == nil || c.Chat() == nil {
		return 0
	}
	return c.Chat().ID
}

func (b *Bot) executeToolCalls(ctx context.Context, c tele.Context, convCtx *conversation.Context, userID string, calls []llm.ToolCall, toolsExposed []string, readSkills []string) agent.ToolExecutionSummary {
	return agent.ExecuteToolCalls(ctx, b.ToolRegistry(), convCtx, userID, ChatIDFromTeleContext(c), calls, b.terminalToolPolicyEnabled(), b.logger,
		agent.WithToolAttemptRecording(identity.RunIDFromContext(ctx), b.ToolAttemptsRepo()))
}

// ExecToolCalls is the exported entry point for channels/telegram.InvocationBuilder.
func (b *Bot) ExecToolCalls(ctx context.Context, c tele.Context, convCtx *conversation.Context, userID string, calls []llm.ToolCall, toolsExposed []string, readSkills []string) agent.ToolExecutionSummary {
	return b.executeToolCalls(ctx, c, convCtx, userID, calls, toolsExposed, readSkills)
}

func (b *Bot) maxToolLoopIterations() int {
	cfg := b.cfg
	maxIterations := config.DefaultAgentLoopMaxSteps
	if cfg != nil && cfg.AgentLoopMaxSteps > 0 {
		maxIterations = cfg.AgentLoopMaxSteps
	}
	if maxIterations < 1 {
		return 1
	}
	return maxIterations
}

// MaxToolLoopIterations is the exported entry point for channels/telegram.InvocationBuilder.
func (b *Bot) MaxToolLoopIterations() int { return b.maxToolLoopIterations() }

func (b *Bot) terminalToolPolicyEnabled() bool {
	cfg := b.cfg
	if cfg == nil {
		return true
	}
	return config.NormalizeTerminalToolPolicy(cfg.TerminalToolPolicy) != "off"
}

// TerminalToolPolicyEnabled is the exported entry point for channels/telegram.InvocationBuilder.
func (b *Bot) TerminalToolPolicyEnabled() bool { return b.terminalToolPolicyEnabled() }
