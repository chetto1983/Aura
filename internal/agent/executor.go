package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/agent/tools/registry"
)

// agentExecutor adapts tools.Registry to ToolExecutor.
// It applies allowlist filtering, per-tool timeouts, and untrusted-content
// wrapping, then adds results to the shared state so subsequent LLM rounds
// see them in the message history.
type agentExecutor struct {
	tools       *tools.Registry
	state       State
	logger      *slog.Logger
	allowlist   []string
	userID      string
	maxChars    int
	toolTimeout time.Duration
}

var _ ToolExecutor = (*agentExecutor)(nil)

func newAgentExecutor(
	reg *tools.Registry,
	state State,
	logger *slog.Logger,
	allowlist []string,
	userID string,
	maxChars int,
	toolTimeout time.Duration,
) *agentExecutor {
	return &agentExecutor{
		tools:       reg,
		state:       state,
		logger:      logger,
		allowlist:   allowlist,
		userID:      userID,
		maxChars:    maxChars,
		toolTimeout: toolTimeout,
	}
}

type toolOutcome struct {
	id      string
	content string
	// awaitingUser is set when the tool returned ErrAwaitingUserInput;
	// the loop handles the pause, no tool_result is added to state.
	awaitingUser *tools.ErrAwaitingUserInput
}

// ExecuteToolCalls runs each call in parallel, wraps results, adds them to
// state (so the next LLM round sees them), and returns an ExecutionSummary.
// Mirrors the fan-out pattern in internal/telegram/conversation_tool_exec.go.
func (e *agentExecutor) ExecuteToolCalls(ctx context.Context, calls []llm.ToolCall) ExecutionSummary {
	outcomes := make([]toolOutcome, len(calls))
	done := make(chan struct{})
	var wg sync.WaitGroup
	for i, call := range calls {
		wg.Add(1)
		// Clone arguments so parallel goroutines cannot race on a shared map
		// (F-002). The clone is shallow — see cloneToolArgs.
		argsClone := cloneToolArgs(call.Arguments)
		callCopy := call
		callCopy.Arguments = argsClone
		go func(i int, call llm.ToolCall) {
			defer wg.Done()
			raw, execErr := e.executeOneTool(ctx, call)
			var awaitErr *tools.ErrAwaitingUserInput
			if errors.As(execErr, &awaitErr) {
				outcomes[i] = toolOutcome{id: call.ID, awaitingUser: awaitErr}
				return
			}
			wrapped := WrapUntrustedToolResult(call.Name, raw)
			outcomes[i] = toolOutcome{id: call.ID, content: limitToolContent(wrapped, e.maxChars)}
		}(i, callCopy)
	}
	go func() {
		wg.Wait()
		close(done)
	}()
	// Honor outer-ctx cancellation so a stuck tool cannot pin Run open after
	// the swarm parent dies or the user disconnects (F-001).
	select {
	case <-done:
	case <-ctx.Done():
		for i, call := range calls {
			if outcomes[i].id == "" {
				outcomes[i] = toolOutcome{id: call.ID, content: tools.FormatToolError(ctx.Err())}
			}
		}
	}

	summary := ExecutionSummary{Results: make(map[string]string, len(outcomes))}
	for _, o := range outcomes {
		if o.awaitingUser != nil {
			// ask_user pause: do NOT add a tool_result to state.
			// The pending tool_call entry already in the assistant message
			// serves as the marker; the resume path injects the tool_result.
			summary.AwaitingUserInput = o.awaitingUser
			continue
		}
		// Add to state so the next LLM round sees the result in message
		// history — the loop only adds stubs for skipped/duplicate calls,
		// not fresh ones (mirrors conversation_tool_exec.go:112).
		e.state.AddToolResultMessage(o.id, o.content)
		summary.LastResult = o.content
		summary.Results[o.id] = o.content
	}
	return summary
}

func (e *agentExecutor) executeOneTool(ctx context.Context, call llm.ToolCall) (string, error) {
	if len(e.allowlist) == 0 || !slices.Contains(e.allowlist, strings.ToLower(call.Name)) {
		return tools.FormatFatalToolError(fmt.Errorf("tool %q is not allowed for this agent", call.Name)), nil
	}
	if e.tools == nil {
		return tools.FormatFatalToolError(errors.New("tool registry unavailable")), nil
	}
	toolCtx := ctx
	var cancel context.CancelFunc
	if e.toolTimeout > 0 {
		toolCtx, cancel = context.WithTimeout(toolCtx, e.toolTimeout)
		defer cancel()
	}
	if strings.TrimSpace(e.userID) != "" {
		toolCtx = tools.WithUserID(toolCtx, e.userID)
	}
	out, err := e.tools.Execute(toolCtx, call.Name, call.Arguments)
	if err != nil {
		var awaitErr *tools.ErrAwaitingUserInput
		if errors.As(err, &awaitErr) {
			return "", awaitErr // propagate sentinel without formatting
		}
		if e.logger != nil {
			e.logger.Warn("agent tool call failed", "tool", call.Name, "error", redactToolError(err))
		}
		return tools.FormatToolError(err), nil
	}
	return out, nil
}

// cloneToolArgs returns a shallow copy of the LLM-supplied arguments map so
// parallel tool executions cannot race on shared map mutation (F-002).
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

// limitToolContent truncates content to maxChars runes. Zero means unlimited.
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

// redactToolError scrubs known credential patterns from a tool error before
// logging. The full error still goes to the LLM via FormatToolError (F-038).
func redactToolError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	msg = redactURLCredentialsRe.ReplaceAllString(msg, "$1<redacted>")
	msg = redactBase64BlobRe.ReplaceAllString(msg, "<base64>")
	return msg
}

var (
	redactURLCredentialsRe = regexp.MustCompile(`([?&](?i:token|api[_-]?key|secret|auth|bearer)=)[^&\s"]+`)
	redactBase64BlobRe     = regexp.MustCompile(`[A-Za-z0-9+/]{40,}={0,2}`)
)
