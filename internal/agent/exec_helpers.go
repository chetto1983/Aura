package agent

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"

	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
)

// ToolRunner abstracts tool dispatch for ExecuteToolCalls.
// *tools.Registry satisfies this interface.
type ToolRunner interface {
	Names() []string
	Execute(ctx context.Context, name string, args map[string]any) (string, error)
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
) ToolExecutionSummary {
	if len(calls) == 0 {
		return ToolExecutionSummary{}
	}

	type outcome struct {
		id            string
		tool          string
		content       string
		readSkillName string
		terminalTool  string
	}
	results := make([]outcome, len(calls))

	var wg sync.WaitGroup
	for i, tc := range calls {
		wg.Add(1)
		go func(i int, tc llm.ToolCall) {
			defer wg.Done()
			toolCtx := tools.WithAllowedToolNames(tools.WithUserID(ctx, userID), runner.Names())
			args := ToolArgumentsForTool(tc.Name, tc.Arguments, chatID)
			result, err := runner.Execute(toolCtx, tc.Name, args)
			if err != nil {
				result = tools.FormatToolError(err)
				if logger != nil {
					logger.Warn("tool call failed", "user_id", userID, "tool", tc.Name, "error", err)
				}
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
		wrapped := WrapUntrustedToolResult(r.tool, r.content)
		convCtx.AddToolResultMessage(r.id, wrapped)
		summary.LastResult = r.content
		summary.Results[r.id] = r.content
		if r.readSkillName != "" {
			summary.ReadSkillNames = toolExecAppendUnique(summary.ReadSkillNames, r.readSkillName)
		}
		if summary.TerminalTool == "" && r.terminalTool != "" {
			summary.TerminalTool = r.terminalTool
		}
	}
	return summary
}

func toolExecAppendUnique(values []string, additions ...string) []string {
	for _, addition := range additions {
		addition = strings.TrimSpace(addition)
		if addition == "" || slices.Contains(values, addition) {
			continue
		}
		values = append(values, addition)
	}
	return values
}
