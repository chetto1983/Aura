package agent

import (
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/reasoningtrace"
)

const redactedReasoningDelta = "[reasoning redacted]"

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

// reasoningChunkEvent is one streamed chain-of-thought delta (amendment #57). It
// mirrors chunkEvent — same trace-identity stamping — but sets LLMResponse.Reasoning
// (not Content) so the reasoning is stream-only and never enters accumulated content.
func (a *LlmAgent) reasoningChunkEvent(ic InvocationContext, spanID [8]byte, parentSpanID *[8]byte, text string) *Event {
	ev := a.newEvent(ic, spanID, parentSpanID)
	reasoning := redactedReasoningDelta
	if a.cfg.ShowReasoning {
		reasoning = text
	}
	ev.LLMResponse = &LLMResponse{Reasoning: reasoning}
	reasoningtrace.Record("agent_event_reasoning_created", map[string]any{
		"request_id": ic.RequestID.String(),
		"thread_id":  a.sessionID,
		"branch":     ic.Branch,
		"chars":      reasoningtrace.RuneLen(text),
		"redacted":   !a.cfg.ShowReasoning,
	})
	return ev
}

// discardStreamedEvent is the mid-stream-retry repudiation signal (B-12): a marker
// Event (no LLMResponse) whose Actions.DiscardStreamed tells a streaming consumer to
// drop the partial chunks the failed attempt already showed, so the retry renders
// over a blank slate. Emitted once, right before the retry re-enters the loop.
func (a *LlmAgent) discardStreamedEvent(ic InvocationContext, spanID [8]byte, parentSpanID *[8]byte) *Event {
	ev := a.newEvent(ic, spanID, parentSpanID)
	ev.Actions.DiscardStreamed = true
	return ev
}

// toolCallEvent announces a finalized tool call before dispatch (D-12 activity).
func (a *LlmAgent) toolCallEvent(ic InvocationContext, spanID [8]byte, parentSpanID *[8]byte, call llm.ToolCall) *Event {
	ev := a.newEvent(ic, spanID, parentSpanID)
	ev.LLMResponse = &LLMResponse{ToolCalls: []llm.ToolCall{call}}
	return ev
}

func (a *LlmAgent) toolStartEvent(ic InvocationContext, spanID [8]byte, parentSpanID *[8]byte,
	call llm.ToolCall, startedAt time.Time, batchIndex, batchSize int,
) *Event {
	ev := a.newEvent(ic, spanID, parentSpanID)
	ts := startedAt.UTC()
	ev.Actions.ToolInvocation = &ToolInvocation{
		Event:      ToolInvocationStart,
		ToolCallID: call.ID,
		ToolName:   call.Function.Name,
		Arguments:  call.Function.Arguments,
		ArgsBytes:  len(call.Function.Arguments),
		BatchIndex: batchIndex,
		BatchSize:  batchSize,
		StartedAt:  &ts,
	}
	return ev
}

// toolResultEvent carries the RoleTool preview threaded back into history. The
// tool_call_id correlation lives in the appended RoleTool history message (Event
// has no dedicated tool_call_id field this phase — AG-UI fan-out is Phase 12); the
// id is passed for forward-compat callers but not stamped on the Event yet.
func (a *LlmAgent) toolResultEvent(ic InvocationContext, spanID [8]byte, parentSpanID *[8]byte, run toolRunResult) *Event {
	ev := a.newEvent(ic, spanID, parentSpanID)
	recordToolDuration(run.EndedAt.Sub(run.StartedAt))
	// The event projection surfaces the RAW tool preview (run.Result.Preview), not
	// the nonce-wrapped prompt rendering (run.Preview): the UI/REPL/AG-UI consumer
	// and the audit ToolInvocation want the clean tool output, while only the
	// model-facing history carries the untrusted envelope (AG-052).
	rawPreview := run.Result.Preview
	ev.LLMResponse = &LLMResponse{Content: rawPreview}
	// Stamp the tool_call_id so a prose consumer (the REPL renderer) can tell this
	// raw tool-result preview apart from a streamed assistant chunk — both carry
	// the text in LLMResponse.Content with no FinishReason, so without this marker
	// the renderer would stream the raw preview AND let it pollute the prose
	// buffer, double-printing the final answer. It is also the AG-UI correlation
	// id a Phase-12 gateway fans out.
	ev.Actions.StateDelta = map[string]any{"tool_call_id": run.ToolCallID}
	startedAt := run.StartedAt.UTC()
	endedAt := run.EndedAt.UTC()
	status := "ok"
	if run.Err != "" {
		status = "error"
	}
	ev.Actions.ToolInvocation = &ToolInvocation{
		Event:             ToolInvocationEnd,
		ToolCallID:        run.ToolCallID,
		ToolName:          run.ToolName,
		Arguments:         run.Arguments,
		ArgsBytes:         len(run.Arguments),
		StartedAt:         &startedAt,
		EndedAt:           &endedAt,
		DurationMS:        run.EndedAt.Sub(run.StartedAt).Milliseconds(),
		Status:            status,
		Error:             run.Err,
		ResultPreview:     rawPreview,
		PreviewBytes:      len(rawPreview),
		ResultBytes:       run.Result.Bytes,
		ResultTruncated:   run.Result.Truncated,
		ResultSidecarPath: run.Result.FullPath,
		ExitCode:          exitCodeFromMeta(run.Result.Meta),
		Meta:              toolResultMetaMap(run.Result.Meta),
	}
	// A tool (send_file) may carry a channel-agnostic artifact descriptor on its
	// Meta. Lift it onto Actions.ArtifactDelta (the named emit seam D-06): this is
	// the ONLY route that populates the forward-compat ArtifactDelta field, and the
	// AG-UI translator's artifact branch keys off it. Purely additive — a run
	// without the key leaves ArtifactDelta nil, so every existing event is unchanged.
	if art, ok := metaArtifact(run.Result.Meta); ok {
		ev.Actions.ArtifactDelta = art
	}
	return ev
}

// metaArtifact pulls the `artifact` descriptor off a ToolResult.Meta map, mirroring
// toolResultMetaMap/exitCodeFromMeta. It reports ok=false when the meta is nil, the
// key is absent, or the value is not a map[string]any — so a malformed meta never
// produces a bogus ArtifactDelta.
func metaArtifact(meta *tools.ToolResultMeta) (map[string]any, bool) {
	if meta == nil {
		return nil, false
	}
	v, ok := (*meta)["artifact"]
	if !ok {
		return nil, false
	}
	art, ok := v.(map[string]any)
	if !ok {
		return nil, false
	}
	return art, true
}

func (a *LlmAgent) toolPreviewEvent(ic InvocationContext, spanID [8]byte, parentSpanID *[8]byte, toolCallID, preview string) *Event {
	ev := a.newEvent(ic, spanID, parentSpanID)
	ev.LLMResponse = &LLMResponse{Content: preview}
	ev.Actions.StateDelta = map[string]any{"tool_call_id": toolCallID}
	return ev
}

func toolResultMetaMap(meta *tools.ToolResultMeta) map[string]any {
	if meta == nil {
		return nil
	}
	out := make(map[string]any, len(*meta))
	for k, v := range *meta {
		out[k] = v
	}
	return out
}

func exitCodeFromMeta(meta *tools.ToolResultMeta) *int {
	if meta == nil {
		return nil
	}
	v, ok := (*meta)["exit_code"]
	if !ok {
		return nil
	}
	switch n := v.(type) {
	case int:
		return &n
	case int32:
		out := int(n)
		return &out
	case int64:
		out := int(n)
		return &out
	case float64:
		out := int(n)
		return &out
	default:
		return nil
	}
}

// finalEvent is the turn's terminal answer (text_response or content-stop
// fallback, D-13). It carries the finish_reason so the REPL can render the
// truncation path (D-21) and surfaces the per-turn token+cost usage in
// Actions.StateDelta["usage"] so the REPL can render the cost footer (D-11/Req#12)
// — the span (tracing.go) is the observability primitive, but the REPL has no span
// reader, so the human-facing footer reads usage off the final Event.
func (a *LlmAgent) finalEvent(ic InvocationContext, spanID [8]byte, parentSpanID *[8]byte,
	requestID, answer, finish string, usage llm.Usage,
) *Event {
	_ = requestID
	ev := a.newEvent(ic, spanID, parentSpanID)
	ev.LLMResponse = &LLMResponse{Content: answer, FinishReason: finish}
	ev.Actions.StateDelta = usageStateDelta(usage)
	return ev
}

// usageStateDelta projects an llm.Usage onto the StateDelta map the final Event
// carries for the REPL cost footer. Cost is included only when the provider sent
// one (a nil Cost stays absent so the footer falls back to the price table rather
// than reading a fabricated zero).
func usageStateDelta(usage llm.Usage) map[string]any {
	d := map[string]any{
		"prompt_tokens":     usage.PromptTokens,
		"completion_tokens": usage.CompletionTokens,
		"cache_hit_tokens":  usage.CachedTokens,
	}
	if usage.Cost != nil {
		d["cost_usd"] = *usage.Cost
	}
	return d
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
