package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/aura/aura/internal/agent/tools/attempts"
	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/stringx"
)

// ToolRunner abstracts tool dispatch for ExecuteToolCalls.
// *tools.Registry satisfies this interface.
type ToolRunner interface {
	Names() []string
	Execute(ctx context.Context, name string, args map[string]any) (string, error)
}

type executeToolCallsConfig struct {
	runID             string
	attemptsRepo      attempts.Repo
	tokenJuiceEnabled bool
}

// ExecuteToolCallsOption configures channel-neutral tool execution helpers.
type ExecuteToolCallsOption func(*executeToolCallsConfig)

// WithToolAttemptRecording enables tool_attempts persistence for custom
// channel executors that do not use the default agentExecutor.
func WithToolAttemptRecording(runID string, repo attempts.Repo) ExecuteToolCallsOption {
	return func(cfg *executeToolCallsConfig) {
		cfg.runID = runID
		cfg.attemptsRepo = repo
	}
}

// WithTokenJuice enables or disables rule-driven output compaction for this
// call batch. When enabled, each tool result is passed through tokenjuice.Compact
// before WrapUntrustedToolResult, reducing context pressure on heavy turns.
func WithTokenJuice(enabled bool) ExecuteToolCallsOption {
	return func(cfg *executeToolCallsConfig) {
		cfg.tokenJuiceEnabled = enabled
	}
}

// ExecuteToolCalls fans out calls concurrently, appends tool results to convCtx
// in original order, and returns a summary. Channel-neutral: chatID is passed
// explicitly so no tele.Context dependency is needed by callers.
func ExecuteToolCalls(
	ctx context.Context,
	runner ToolRunner,
	convCtx *conversation.Context,
	userID string,
	chatID int64,
	calls []llm.ToolCall,
	terminalPolicyEnabled bool,
	logger *slog.Logger,
	options ...ExecuteToolCallsOption,
) ToolExecutionSummary {
	if len(calls) == 0 {
		return ToolExecutionSummary{}
	}
	cfg := executeToolCallsConfig{}
	for _, option := range options {
		if option != nil {
			option(&cfg)
		}
	}

	type outcome struct {
		id            string
		tool          string
		content       string
		readSkillName string
		terminalTool  string
		awaitingUser  *tools.ErrAwaitingUserInput
	}
	results := make([]outcome, len(calls))

	var wg sync.WaitGroup
	for i, tc := range calls {
		wg.Add(1)
		go func(i int, tc llm.ToolCall) {
			defer wg.Done()
			startedAt := time.Now()
			conversationID := userID
			if chatID > 0 {
				conversationID = fmt.Sprintf("%d", chatID)
			}
			toolCtx := tools.WithConversationID(tools.WithAllowedToolNames(tools.WithUserID(ctx, userID), runner.Names()), conversationID)
			args := ToolArgumentsForTool(tc.Name, tc.Arguments, chatID)
			result, err := runner.Execute(toolCtx, tc.Name, args)
			class := ""
			var awaitErr *tools.ErrAwaitingUserInput
			if errors.As(err, &awaitErr) {
				awaitErr.ToolCallID = tc.ID
				RecordToolAttempt(ctx, logger, cfg.attemptsRepo, ToolAttemptRecordInput{
					RunID:     cfg.runID,
					ToolName:  tc.Name,
					Arguments: args,
					StartedAt: startedAt,
					Elapsed:   time.Since(startedAt),
					Class:     "cancelled",
				})
				results[i] = outcome{id: tc.ID, tool: tc.Name, awaitingUser: awaitErr}
				return
			}
			if err != nil {
				class = tools.ClassifyToolError(err)
				result = tools.FormatToolError(err)
				if logger != nil {
					logger.Warn("tool call failed", "user_id", userID, "tool", tc.Name, "error", err)
				}
			}
			RecordToolAttempt(ctx, logger, cfg.attemptsRepo, ToolAttemptRecordInput{
				RunID:     cfg.runID,
				ToolName:  tc.Name,
				Arguments: args,
				StartedAt: startedAt,
				Elapsed:   time.Since(startedAt),
				Class:     class,
				Err:       err,
			})
			if err == nil && cfg.tokenJuiceEnabled {
				result = CompactToolOutput(logger, tc.Name, args, result)
			}
			readSkillName := ""
			if err == nil && tc.Name == "read_file" {
				readSkillName = SkillNameFromReadFileArgs(tc.Arguments)
			}
			terminalTool := ""
			if err == nil && terminalPolicyEnabled && IsTerminalTool(tc.Name) {
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

	summary := ToolExecutionSummary{Results: make(map[string]string, len(results))}
	for _, r := range results {
		if r.awaitingUser != nil {
			summary.AwaitingUserInput = r.awaitingUser
			continue
		}
		wrapped := WrapUntrustedToolResult(r.tool, r.content)
		convCtx.AddToolResultMessage(r.id, wrapped)
		summary.LastResult = r.content
		summary.Results[r.id] = r.content
		if r.readSkillName != "" {
			summary.ReadSkillNames = stringx.AppendUnique(summary.ReadSkillNames, r.readSkillName)
		}
		if summary.TerminalTool == "" && r.terminalTool != "" {
			summary.TerminalTool = r.terminalTool
		}
	}
	return summary
}
