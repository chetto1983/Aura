package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

// llm_agent_tool.go holds the single-tool execution + terminal-call helpers split out
// of llm_agent.go (deep-refactor-on-touch / ≤600 LOC, CLAUDE.md): runTerminal handles
// the text_response terminal, runTool dispatches one call with its span + spillover +
// swarm ctx, and the small history-append helpers keep the wire valid on reject/finalize.
// The batch orchestration that calls these lives in llm_agent_dispatch.go.

// runTerminal handles the text_response terminal call (D-13). A malformed payload
// feeds the parse error back and continues the loop (done=false); the completion gate
// (D-43) can veto on a side-effecting turn by appending a RoleTool "not done" result
// and continuing; otherwise it appends the final answer and emits the terminal
// finalEvent (done=true). The runnable siblings have already executed this turn, so —
// unlike the old sequential path — there are never later siblings left to synthesize.
func (a *LlmAgent) runTerminal(ic InvocationContext, spanID [8]byte, parentSpanID *[8]byte,
	requestID string, call llm.ToolCall, usage llm.Usage, yield func(*Event, error) bool,
) (done bool) {
	answer, perr := parseTextResponse(call.Function.Arguments)
	if perr != nil {
		a.appendToolError(call.ID, perr)
		// yield false (consumer stopped) → done=true; otherwise continue the loop.
		return !yield(a.toolPreviewEvent(ic, spanID, parentSpanID, call.ID, "parse error"), nil)
	}
	if veto, feedback := a.gateCompletion(ic, answer); veto {
		a.history = append(a.history, llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Content: feedback})
		return !yield(a.toolPreviewEvent(ic, spanID, parentSpanID, call.ID, "completion gate: not done"), nil)
	}
	a.history = append(a.history, llm.Message{Role: llm.RoleAssistant, Content: answer})
	yield(a.finalEvent(ic, spanID, parentSpanID, requestID, answer, "stop", usage), nil)
	return true
}

func (a *LlmAgent) appendSyntheticToolResults(calls []llm.ToolCall, content string) {
	for _, call := range calls {
		if call.ID == "" {
			continue
		}
		a.history = append(a.history, llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: call.ID,
			Content:    content,
		})
	}
}

type toolRunResult struct {
	ToolCallID string
	ToolName   string
	Arguments  string
	StartedAt  time.Time
	EndedAt    time.Time
	Preview    string
	Result     tools.ToolResult
	Err        string
	// Mutating mirrors the dispatched tool's Spec().Mutating so dispatch can flag
	// the run as side-effecting (D-43) without re-resolving the spec.
	Mutating bool
}

func hookToolRunResult(call llm.ToolCall, startedAt time.Time, res tools.ToolResult) toolRunResult {
	if res.Bytes == 0 && res.Preview != "" {
		res.Bytes = len(res.Preview)
	}
	now := time.Now().UTC()
	return toolRunResult{
		ToolCallID: call.ID,
		ToolName:   call.Function.Name,
		Arguments:  call.Function.Arguments,
		StartedAt:  startedAt,
		EndedAt:    now,
		Preview:    renderToolResultForPrompt(call.Function.Name, res),
		Result:     res,
	}
}

// runTool dispatches one tool call and returns the RoleTool history content. A
// missing tool or an Execute error becomes an error preview the model sees (D-15),
// never a panic. The shared spillover ctx is injected so large outputs page to a
// sidecar (D-25); the swarm ctx is also injected so a swarm_spawn dispatch can read
// the parent budget/registry/client/config off the ctx (the cycle-free seam — only
// swarm_spawn reads the key, every other tool ignores it).
func (a *LlmAgent) runTool(ctx context.Context, budget *Budget, call llm.ToolCall, startedAt time.Time) toolRunResult {
	run := toolRunResult{
		ToolCallID: call.ID,
		ToolName:   call.Function.Name,
		Arguments:  call.Function.Arguments,
		StartedAt:  startedAt,
	}
	tool, ok := a.registry.Get(call.Function.Name)
	if !ok {
		// An unknown tool still gets a tool.execute span (O-08) so the trace shows the
		// failed dispatch; mutating is unknown here, so it stays false.
		_, span := startToolSpan(ctx, call.Function.Name, false)
		run.EndedAt = time.Now().UTC()
		run.Err = fmt.Sprintf("unknown tool %q", call.Function.Name)
		recordToolError(call.Function.Name)
		slog.Error("agent tool error", "tool", call.Function.Name, "tool_call_id", call.ID, "err", run.Err)
		run.Preview = "error: " + run.Err
		run.Result = tools.ToolResult{Preview: run.Preview, Bytes: len(run.Preview)}
		endToolSpan(span, run.Err)
		return run
	}
	// Dispatch gate (full-promotion parity safety net): a deferred tool whose schema
	// has not been loaded via tool_search is NOT in the callable manifest, so a call to
	// it is hallucinated. Bounce it back with load-it-first guidance instead of executing
	// with fabricated arguments — model-visible guidance, not an error span.
	if a.isDeferredUnloaded(call.Function.Name) {
		_, span := startToolSpan(ctx, call.Function.Name, false)
		run.EndedAt = time.Now().UTC()
		run.Preview = fmt.Sprintf(
			"error: tool_not_loaded: %q is a deferred tool whose schema is not loaded; call tool_search with query %q to load it, then call %s with the loaded parameters",
			call.Function.Name, "select:"+call.Function.Name, call.Function.Name)
		run.Result = tools.ToolResult{Preview: run.Preview, Bytes: len(run.Preview)}
		endToolSpan(span, "")
		return run
	}
	run.Mutating = tool.Spec().Mutating
	// tool.execute span (O-08): one per dispatch, nested under agent.turn via ctx so a
	// trace shows per-tool latency. Concurrent (executeBatch) dispatches each start
	// their own span off the shared turn ctx — otel spans are safe to start in parallel.
	spanCtx, span := startToolSpan(ctx, call.Function.Name, run.Mutating)
	toolCtx := tools.WithToolCallContext(spanCtx, a.sessionID, call.ID, a.runDir, a.previewCap)
	toolCtx = WithSwarmContext(toolCtx, budget, a.registry, a.client, a.cfg, a.sessionID, a.gateway)
	if d := budget.NodeTimeout(); d > 0 {
		var cancel context.CancelFunc
		toolCtx, cancel = context.WithTimeout(toolCtx, d)
		defer cancel()
	}
	res, err := a.execTool(toolCtx, tool, run.Mutating, json.RawMessage(call.Function.Arguments))
	run.EndedAt = time.Now().UTC()
	if err != nil {
		run.Err = err.Error()
		recordToolError(call.Function.Name)
		slog.Error("agent tool error", "tool", call.Function.Name, "tool_call_id", call.ID, "err", err)
		// An Execute error (and the ErrAwaitingUserInput pause sentinel rendered as
		// `error: awaiting user input`) is an agent-synthesized control-plane string,
		// not external tool content — it is NOT wrapped in the untrusted envelope.
		run.Preview = "error: " + run.Err
		run.Result = tools.ToolResult{Preview: run.Preview, Bytes: len(run.Preview)}
		endToolSpan(span, run.Err)
		return run
	}
	run.Result = res
	run.Preview = renderToolResultForPrompt(call.Function.Name, res)
	endToolSpan(span, "")
	return run
}

// appendToolError appends a RoleTool error message for a malformed terminal-tool
// call so the model self-corrects (D-15).
func (a *LlmAgent) appendToolError(callID string, err error) {
	a.history = append(a.history, llm.Message{
		Role: llm.RoleTool, ToolCallID: callID, Content: "error: " + err.Error(),
	})
}
