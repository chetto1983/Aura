package agent

import (
	"time"

	"github.com/chetto1983/aura/internal/llm"
)

// newEvent stamps the trace identity (Author/Branch/RequestID/SpanID/ParentSpanID/
// ThreadID) and a non-zero UTC Timestamp common to every Event LlmAgent emits
// (Req#14 — Phase 2 left Timestamp zero; the agent must set it). Concern-split out
// of llm_agent.go to keep that file focused on the loop (≤600 LOC).
func (a *LlmAgent) newEvent(ic InvocationContext, spanID [8]byte, parentSpanID *[8]byte) *Event {
	return &Event{
		Author:       a.name,
		Branch:       ic.Branch,
		RequestID:    ic.RequestID,
		SpanID:       spanID,
		ParentSpanID: parentSpanID,
		ThreadID:     a.sessionID,
		Timestamp:    time.Now().UTC(),
	}
}

// chunkEvent is one streamed assistant text delta (Req#11).
func (a *LlmAgent) chunkEvent(ic InvocationContext, spanID [8]byte, parentSpanID *[8]byte, text string) *Event {
	ev := a.newEvent(ic, spanID, parentSpanID)
	ev.LLMResponse = &LLMResponse{Content: text}
	return ev
}

// toolCallEvent announces a finalized tool call before dispatch (D-12 activity).
func (a *LlmAgent) toolCallEvent(ic InvocationContext, spanID [8]byte, parentSpanID *[8]byte, call llm.ToolCall) *Event {
	ev := a.newEvent(ic, spanID, parentSpanID)
	ev.LLMResponse = &LLMResponse{ToolCalls: []llm.ToolCall{call}}
	return ev
}

// toolResultEvent carries the RoleTool preview threaded back into history. The
// tool_call_id correlation lives in the appended RoleTool history message (Event
// has no dedicated tool_call_id field this phase — AG-UI fan-out is Phase 12); the
// id is passed for forward-compat callers but not stamped on the Event yet.
func (a *LlmAgent) toolResultEvent(ic InvocationContext, spanID [8]byte, parentSpanID *[8]byte, toolCallID, preview string) *Event {
	_ = toolCallID
	ev := a.newEvent(ic, spanID, parentSpanID)
	ev.LLMResponse = &LLMResponse{Content: preview}
	return ev
}

// finalEvent is the turn's terminal answer (text_response or content-stop
// fallback, D-13). It carries the finish_reason so the REPL can render the
// truncation path (D-21).
func (a *LlmAgent) finalEvent(ic InvocationContext, spanID [8]byte, parentSpanID *[8]byte,
	requestID, answer, finish string, usage llm.Usage,
) *Event {
	_ = requestID
	_ = usage
	ev := a.newEvent(ic, spanID, parentSpanID)
	ev.LLMResponse = &LLMResponse{Content: answer, FinishReason: finish}
	return ev
}

// terminalBudgetEvent is the budget-trip terminal Event (D-04): an explicit
// termination signal, NEVER the iter.Seq2 error slot. reason ∈ {max_steps,
// wallclock, dedup}.
func (a *LlmAgent) terminalBudgetEvent(ic InvocationContext, spanID [8]byte, parentSpanID *[8]byte, reason string) *Event {
	ev := a.newEvent(ic, spanID, parentSpanID)
	ev.Actions = Actions{
		Escalate: true,
		StateDelta: map[string]any{
			"termination_reason": "budget_exhausted",
			"limit_hit":          reason,
		},
	}
	return ev
}
