package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/agent/governance"
	"github.com/aura/aura/internal/agent/tools/attempts"
	toolregistry "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/llm"
)

type webToolExecutor struct {
	tools                 *toolregistry.Registry
	state                 agent.State
	logger                *slog.Logger
	allowlist             []string
	userID                string
	conversationID        string
	runID                 string
	attemptsRepo          attempts.Repo
	maxChars              int
	toolTimeout           time.Duration
	terminalPolicyEnabled bool
	tokenJuiceEnabled     bool
	payloadSummarizer     governance.PayloadSummarizer
	invalidatePinned      func()
}

func (e *webToolExecutor) ExecuteToolCalls(ctx context.Context, calls []llm.ToolCall) agent.ExecutionSummary {
	type outcome struct {
		id        string
		tool      string
		content   string
		awaiting  *toolregistry.ErrAwaitingUserInput
		readSkill string
		terminal  string
	}
	outcomes := make([]outcome, len(calls))
	pinnedWrite := agent.PinnedOperationalWriteInCalls(calls)
	done := make(chan struct{})
	var wg sync.WaitGroup
	for i, call := range calls {
		wg.Add(1)
		callCopy := call
		callCopy.Arguments = cloneToolArgs(call.Arguments)
		go func(i int, call llm.ToolCall) {
			defer wg.Done()
			startedAt := time.Now()
			raw, executedArgs, class, err := e.executeOne(ctx, call)
			var awaitErr *toolregistry.ErrAwaitingUserInput
			if errors.As(err, &awaitErr) {
				awaitErr.ToolCallID = call.ID
				agent.RecordToolAttempt(ctx, e.logger, e.attemptsRepo, agent.ToolAttemptRecordInput{
					RunID:     e.runID,
					ToolName:  call.Name,
					Arguments: executedArgs,
					StartedAt: startedAt,
					Elapsed:   time.Since(startedAt),
					Class:     "cancelled",
				})
				outcomes[i] = outcome{id: call.ID, awaiting: awaitErr}
				return
			}
			content := raw
			if err != nil {
				if class == "" {
					class = toolregistry.ClassifyToolError(err)
				}
				content = toolregistry.FormatToolError(err)
				if e.logger != nil {
					e.logger.Warn("web chat tool call failed", "tool", call.Name, "error", err)
				}
			}
			agent.RecordToolAttempt(ctx, e.logger, e.attemptsRepo, agent.ToolAttemptRecordInput{
				RunID:     e.runID,
				ToolName:  call.Name,
				Arguments: executedArgs,
				StartedAt: startedAt,
				Elapsed:   time.Since(startedAt),
				Class:     class,
				Err:       err,
			})
			if err == nil && e.tokenJuiceEnabled {
				content = agent.CompactToolOutput(e.logger, call.Name, executedArgs, content)
			}
			if err == nil && e.payloadSummarizer != nil {
				if sp := e.payloadSummarizer.MaybeSummarize(ctx, call.Name, "", content); sp != nil {
					content = sp.Summary
				}
			}
			wrapped := agent.WrapUntrustedToolResult(call.Name, content)
			outcomes[i] = outcome{
				id:        call.ID,
				tool:      call.Name,
				content:   limitToolContent(wrapped, e.maxChars),
				readSkill: agent.SkillNameFromReadFileArgs(call.Arguments),
				terminal:  terminalToolName(call.Name, class == "" && err == nil && e.terminalPolicyEnabled),
			}
		}(i, callCopy)
	}
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		for i, call := range calls {
			if outcomes[i].id == "" {
				outcomes[i] = outcome{id: call.ID, content: toolregistry.FormatToolError(ctx.Err())}
			}
		}
	}
	if pinnedWrite && e.invalidatePinned != nil {
		e.invalidatePinned()
	}
	summary := agent.ExecutionSummary{Results: make(map[string]string, len(outcomes))}
	for _, item := range outcomes {
		if item.awaiting != nil {
			summary.AwaitingUserInput = item.awaiting
			continue
		}
		e.state.AddToolResultMessage(item.id, item.content)
		summary.LastResult = item.content
		summary.Results[item.id] = item.content
		if item.readSkill != "" && !slices.Contains(summary.ReadSkillNames, item.readSkill) {
			summary.ReadSkillNames = append(summary.ReadSkillNames, item.readSkill)
		}
		if summary.TerminalTool == "" && item.terminal != "" {
			summary.TerminalTool = item.terminal
		}
	}
	return summary
}


func (e *webToolExecutor) executeOne(ctx context.Context, call llm.ToolCall) (string, map[string]any, string, error) {
	args := call.Arguments
	if len(e.allowlist) == 0 || !slices.Contains(e.allowlist, strings.ToLower(call.Name)) {
		return toolregistry.FormatFatalToolError(fmt.Errorf("tool %q is not allowed for this agent", call.Name)), args, "permission", nil
	}
	if e.tools == nil {
		return toolregistry.FormatFatalToolError(errors.New("tool registry unavailable")), args, "error", nil
	}
	toolCtx := ctx
	var cancel context.CancelFunc
	if e.toolTimeout > 0 {
		toolCtx, cancel = context.WithTimeout(toolCtx, e.toolTimeout)
		defer cancel()
	}
	toolCtx = toolregistry.WithAllowedToolNames(toolCtx, e.allowlist)
	if e.userID != "" {
		toolCtx = toolregistry.WithUserID(toolCtx, e.userID)
		convID := e.conversationID
		if convID == "" {
			convID = e.userID
		}
		toolCtx = toolregistry.WithConversationID(toolCtx, convID)
	}
	out, err := e.tools.Execute(toolCtx, call.Name, args)
	if err != nil {
		return out, args, toolregistry.ClassifyToolError(err), err
	}
	return out, args, "", nil
}

func cleanWebToolList(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func terminalToolName(name string, ok bool) string {
	if ok && agent.IsTerminalTool(name) {
		return name
	}
	return ""
}

func cloneToolArgs(args map[string]any) map[string]any {
	if args == nil {
		return nil
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		out[k] = v
	}
	return out
}

func limitToolContent(content string, maxChars int) string {
	if maxChars <= 0 {
		return content
	}
	runes := []rune(content)
	if len(runes) <= maxChars {
		return content
	}
	if maxChars <= 16 {
		return string(runes[:maxChars])
	}
	return string(runes[:maxChars-15]) + "\n...[truncated]"
}
