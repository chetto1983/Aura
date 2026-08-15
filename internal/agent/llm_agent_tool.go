package agent

import (
	"context"
	"encoding/json"
	"errors"
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
// feeds the parse error back and continues the loop (done=false); the verification gate
// and then the completion gate (D-43) can each send the turn back by appending a
// RoleTool result and continuing; otherwise it appends the final answer and emits the
// terminal finalEvent (done=true). The runnable siblings have already executed this
// turn, so — unlike the old sequential path — there are never later siblings left to
// synthesize.
//
// This is the terminal a tool-calling model actually reaches; the content-stop seam in
// llm_agent.go is the D-13/D-16 fallback for a model that answers in prose instead. Both
// are voluntary terminations, so both gates run at both — and the attempt counters are on
// the agent, so the budget is per RUN and shared across the two seams, never per seam.
func (a *LlmAgent) runTerminal(ic InvocationContext, spanID [8]byte, parentSpanID *[8]byte,
	requestID string, call llm.ToolCall, acc *turnUsage, yield func(*Event, error) bool,
) (done bool) {
	answer, perr := parseTextResponse(call.Function.Arguments)
	if perr != nil {
		a.appendToolError(call.ID, perr)
		// yield false (consumer stopped) → done=true; otherwise continue the loop.
		return !yield(a.toolPreviewEvent(ic, spanID, parentSpanID, call.ID, "parse error"), nil)
	}
	// Verification gate first, for the reason it goes first at the content-stop seam:
	// it is deterministic and free, and the critic below costs a model call. Unlike
	// that seam there IS a tool_call to answer here, so the nudge rides a RoleTool
	// result on the terminal call id — a RoleUser message would leave this
	// text_response unanswered and break the wire.
	if nudge, ok := a.gateVerification(); ok {
		a.history = append(a.history, llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Content: nudge})
		return !yield(a.toolPreviewEvent(ic, spanID, parentSpanID, call.ID, "verification gate: unverified edit"), nil)
	}
	if veto, feedback := a.gateCompletion(ic, answer); veto {
		a.history = append(a.history, llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Content: feedback})
		return !yield(a.toolPreviewEvent(ic, spanID, parentSpanID, call.ID, "completion gate: not done"), nil)
	}
	a.history = append(a.history, llm.Message{Role: llm.RoleAssistant, Content: answer})
	yield(a.finalEvent(ic, spanID, parentSpanID, requestID, answer, "stop", acc.total()), nil)
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
	ctx, boundaryEnd := startToolObservation(ctx, call.Function.Name)
	var boundaryErr error
	defer boundaryEnd.PanicSafe(&boundaryErr)
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
		boundaryErr = errors.New("unknown tool")
		recordToolError(call.Function.Name)
		slog.Error("agent tool error", "tool", call.Function.Name, "tool_call_id", call.ID, "err", run.Err)
		run.Preview = "error: " + run.Err
		run.Result = tools.ToolResult{Preview: run.Preview, Bytes: len(run.Preview)}
		endToolSpan(span, run.Err)
		return run
	}
	// Dispatch gate (full-promotion parity safety net): a deferred tool whose schema
	// has not been loaded via tool_search is NOT in the callable manifest, so its
	// arguments were written without ever seeing the parameters. Refuse THIS call —
	// but answer with the schema rather than with an errand.
	//
	// The refusal used to say "call tool_search with select:<name>, then call it
	// again", which cost a whole extra LLM round trip to fetch something the process
	// was already holding: registry.Get returned the tool one line above. Measured on
	// a live turn — shell_exec, the most-used tool in the system — the trace read
	//
	//	· shell_exec  →  "I need to load shell_exec first"  →  · tool_search  →  · shell_exec
	//
	// three model turns to run one command. Handing the schema back collapses that to
	// two, and the guard is not weakened but strengthened: the retry is written
	// against the real parameters instead of a name the model had to look up.
	//
	// MetaActivatedTools is the same promotion channel tool_search uses, drained by
	// promoteFromMeta in the SERIAL result loop after executeBatch has joined — so
	// this stays race-free while dispatch runs concurrently.
	if a.isDeferredUnloaded(call.Function.Name) {
		_, span := startToolSpan(ctx, call.Function.Name, false)
		run.EndedAt = time.Now().UTC()
		run.Preview = fmt.Sprintf(
			"error: tool_not_loaded: %q was called before its schema was read, so the arguments "+
				"could not be trusted and the call did NOT run. Its schema follows and the tool is "+
				"now loaded — call %s again using exactly these parameters. Do not call tool_search "+
				"for it.\n\n%s",
			call.Function.Name, call.Function.Name, tools.RenderSpec(tool.Spec()))
		meta := tools.ToolResultMeta{tools.MetaActivatedTools: []string{call.Function.Name}}
		run.Result = tools.ToolResult{Preview: run.Preview, Bytes: len(run.Preview), Meta: &meta}
		endToolSpan(span, "")
		return run
	}
	run.Mutating = tool.Spec().Mutating
	// tool.execute span (O-08): one per dispatch, nested under agent.turn via ctx so a
	// trace shows per-tool latency. Concurrent (executeBatch) dispatches each start
	// their own span off the shared turn ctx — otel spans are safe to start in parallel.
	spanCtx, span := startToolSpan(ctx, call.Function.Name, run.Mutating)
	toolCtx := tools.WithToolCallContext(spanCtx, a.sessionID, call.ID, a.runDir, a.previewCap)
	toolCtx = WithSwarmContext(
		toolCtx,
		budget,
		a.registry,
		a.client,
		a.cfg,
		a.sessionID,
		a.gateway,
	)
	if d := budget.NodeTimeout(); d > 0 {
		var cancel context.CancelFunc
		toolCtx, cancel = context.WithTimeout(toolCtx, d)
		defer cancel()
	}
	res, err := a.execTool(toolCtx, tool, run.Mutating, json.RawMessage(call.Function.Arguments))
	if err == nil {
		// HARN-03 verification seam, disarmed unless AURA_TEST_FORCE_REPLAY_PROBE is set
		// (see llm_agent_replay_probe.go). Re-dispatches this same call once so the
		// same-round retry path — the only reachable replay trigger — runs for real.
		if replayed, ok := a.forceSameRoundReplay(toolCtx, tool, run.Mutating, json.RawMessage(call.Function.Arguments)); ok {
			res = replayed
		}
	}
	run.EndedAt = time.Now().UTC()
	if err != nil {
		boundaryErr = err
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
