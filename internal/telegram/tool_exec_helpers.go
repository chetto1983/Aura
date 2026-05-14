package telegram

import (
	"context"
	"sync"

	"github.com/aura/aura/internal/agent"
	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"

	tele "gopkg.in/telebot.v4"
)

func ChatIDFromTeleContext(c tele.Context) int64 {
	if c == nil || c.Chat() == nil {
		return 0
	}
	return c.Chat().ID
}

// executeToolCalls runs the LLM's tool calls concurrently and appends results
// to convCtx in original call order. Used by both this package's tests and by
// channels/telegram.InvocationBuilder via ExecToolCalls.
func (b *Bot) executeToolCalls(ctx context.Context, c tele.Context, convCtx *conversation.Context, userID string, calls []llm.ToolCall, toolsExposed []string, readSkills []string) agent.ToolExecutionSummary {
	if len(calls) == 0 {
		return agent.ToolExecutionSummary{}
	}

	toolReg := b.ToolRegistry()

	type outcome struct {
		id            string
		tool          string
		content       string
		fatal         bool
		readSkillName string
		terminalTool  string
	}
	results := make([]outcome, len(calls))

	var wg sync.WaitGroup
	for i, tc := range calls {
		wg.Add(1)
		go func(i int, tc llm.ToolCall) {
			defer wg.Done()
			toolCtx := tools.WithAllowedToolNames(tools.WithUserID(ctx, userID), toolReg.Names())
			args := agent.ToolArgumentsForTool(tc.Name, tc.Arguments, ChatIDFromTeleContext(c))
			result, err := toolReg.Execute(toolCtx, tc.Name, args)
			if err != nil {
				result = tools.FormatToolError(err)
				b.logger.Warn("tool call failed", "user_id", userID, "tool", tc.Name, "error", err)
			}
			readSkillName := ""
			if err == nil && tc.Name == "read_file" {
				readSkillName = agent.SkillNameFromReadFileArgs(tc.Arguments)
			}
			terminalTool := ""
			if err == nil && b.terminalToolPolicyEnabled() && agent.IsTerminalTool(tc.Name) {
				terminalTool = tc.Name
			}
			results[i] = outcome{
				id:            tc.ID,
				tool:          tc.Name,
				content:       result,
				readSkillName: readSkillName,
				terminalTool:  terminalTool,
			}
		}(i, tc)
	}
	wg.Wait()

	summary := agent.ToolExecutionSummary{Results: make(map[string]string, len(results))}
	for _, r := range results {
		wrapped := agent.WrapUntrustedToolResult(r.tool, r.content)
		convCtx.AddToolResultMessage(r.id, wrapped)
		summary.LastResult = r.content
		summary.Results[r.id] = r.content
		if r.fatal && summary.FatalResult == "" {
			summary.FatalResult = r.content
		}
		if r.readSkillName != "" {
			summary.ReadSkillNames = appendUniqueStrings(summary.ReadSkillNames, r.readSkillName)
		}
		if summary.TerminalTool == "" && r.terminalTool != "" {
			summary.TerminalTool = r.terminalTool
		}
	}
	return summary
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
