// Pause-DETECTION seam (D-A1-03 / AM-01). This file holds ONLY the half of the
// HITL pause primitive that lives in the agent: catching the
// tools.ErrAwaitingUserInput sentinel, applying intra-turn exclusivity, and
// emitting the pause as an Actions.AwaitingInput Event. The agent stays DB-free —
// pause PERSISTENCE + resume orchestration live in the Runner (04-05), which
// observes this Event and is the sole writer of aura.paused_states. Split out of
// llm_agent.go for separation of concern (AM-01), not size.
//
// The pause is Event-only, NEVER the iter.Seq2 error slot (RESEARCH Pattern-3
// anti-pattern; mirrors budget exhaustion at llm_agent.go terminalBudgetEvent).
package agent

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

// askUserToolName is the canonical ask_user tool name, read from its Spec so a
// rename of the tool cannot silently break pause detection.
var askUserToolName = tools.AskUser{}.Spec().Name

// pauseCalls scans the assistant's batched tool calls and returns the subset that
// are valid ask_user pauses, with each call's ToolCallID stamped onto the carried
// sentinel. Only calls whose tool name is ask_user are pre-executed (so no other
// tool runs twice / side-effects early); a malformed ask_user that fails
// validation is NOT a pause — it returns no sentinel and falls through to normal
// dispatch as a RoleTool error. The returned []llm.ToolCall is the ask_user-only
// rewrite of the assistant message (D-A1-07 OpenAI wire-correctness): siblings are
// dropped and re-emitted by the model on the next round.
func (a *LlmAgent) pauseCalls(ctx context.Context, calls []llm.ToolCall) []pauseCall {
	var out []pauseCall
	for i := range calls {
		call := calls[i]
		if call.Function.Name != askUserToolName {
			continue
		}
		pause, ok := a.detectPause(ctx, call)
		if !ok {
			continue
		}
		pause.ToolCallID = call.ID
		out = append(out, pauseCall{call: call, pause: pause})
	}
	return out
}

// pauseCall pairs a finalized ask_user tool call with its decoded pause sentinel.
type pauseCall struct {
	call  llm.ToolCall
	pause *tools.ErrAwaitingUserInput
}

// pauseToolCalls projects the detected pauses back to the ask_user-only
// []llm.ToolCall that rewrites the assistant message (D-A1-07 wire-correctness).
func pauseToolCalls(pauses []pauseCall) []llm.ToolCall {
	out := make([]llm.ToolCall, len(pauses))
	for i, p := range pauses {
		out[i] = p.call
	}
	return out
}

// detectPause runs the ask_user tool for one call and reports whether it produced
// the pause sentinel. A nil-tool / non-sentinel error means "not a pause" — the
// caller lets normal dispatch surface it (validation errors become RoleTool
// errors the model self-corrects against, D-15). ctx is the live invocation
// context (ic.Ctx) so the pre-execution honors the agent's cancellation, the
// per-child swarm timeout (D-11), and the budget wallclock deadline (D-13) instead
// of running detached on context.Background() (CR-02).
func (a *LlmAgent) detectPause(ctx context.Context, call llm.ToolCall) (*tools.ErrAwaitingUserInput, bool) {
	tool, ok := a.registry.Get(call.Function.Name)
	if !ok {
		return nil, false
	}
	_, err := tool.Execute(ctx, json.RawMessage(call.Function.Arguments))
	var pause *tools.ErrAwaitingUserInput
	if errors.As(err, &pause) {
		return pause, true
	}
	return nil, false
}

// emitPauses yields one pause Event per detected ask_user call, in the order the
// model emitted them. The assistant message has already been rewritten to the
// ask_user-only tool_calls by the caller; NO RoleTool result is appended for a
// pause (the loop is suspended, the Runner injects the answer on resume). The
// yield-after-false guard is honored: once the consumer returns false the loop
// stops yielding (iter.Seq2 contract).
func (a *LlmAgent) emitPauses(ic InvocationContext, spanID [8]byte, parentSpanID *[8]byte,
	pauses []pauseCall, yield func(*Event, error) bool,
) {
	for _, p := range pauses {
		if !yield(a.pauseEvent(ic, spanID, parentSpanID, p.pause), nil) {
			return
		}
	}
}

// pauseEvent builds the HITL pause Event carrying Actions.AwaitingInput (D-A1-03).
// It projects the tools.ErrAwaitingUserInput payload onto the Event model's
// AwaitingInput (no DB type crosses here) and stamps OriginAgent for swarm
// forward-compat (D-A1-08).
func (a *LlmAgent) pauseEvent(ic InvocationContext, spanID [8]byte, parentSpanID *[8]byte,
	pause *tools.ErrAwaitingUserInput,
) *Event {
	ev := a.newEvent(ic, spanID, parentSpanID)
	ev.Actions.AwaitingInput = &AwaitingInput{
		Question:           pause.Question,
		Options:            pauseOptions(pause.Options),
		Kind:               pause.Kind,
		Priority:           pause.Priority,
		ToolCallID:         pause.ToolCallID,
		OriginAgent:        a.name,
		ProxiedFromChildID: pause.ProxiedFromChildID,
		ProxiedToolCallID:  pause.ProxiedToolCallID,
	}
	return ev
}

// pauseOptions copies tools.Option values onto the Event-model PauseOption (the
// model layer does not import tools, avoiding a dependency cycle).
func pauseOptions(opts []tools.Option) []PauseOption {
	if len(opts) == 0 {
		return nil
	}
	out := make([]PauseOption, len(opts))
	for i, o := range opts {
		out[i] = PauseOption{Label: o.Label, Value: o.Value}
	}
	return out
}
