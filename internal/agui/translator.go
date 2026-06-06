package agui

import (
	"iter"
	"sort"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/chetto1983/aura/internal/agent"
)

// Translate maps Aura's per-token *agent.Event stream onto a valid AG-UI
// events.Event sequence. It is a PURE function (no I/O, no goroutines): RUN_STARTED
// first, RUN_FINISHED(success) last, with a chunk-coalescing text-message state
// machine in between. idgen owns id minting so every emitted messageId/toolCallId
// is non-empty (both are Validate()-required by the SDK).
//
// Run-boundary policy (locked OQ1 a): the real (*LlmAgent).Run streams per-token
// chunk Events AND a final Event carrying the full answer + FinishReason. We stream
// the deltas (one TEXT_MESSAGE_CONTENT per non-empty delta) and treat the final
// Event as END-only — its Content is NOT re-emitted as a CONTENT (no double-stream);
// its usage StateDelta becomes a STATE_DELTA event.
func Translate(threadID, runID string, idgen IDGenerator, seq iter.Seq2[*agent.Event, error]) iter.Seq2[events.Event, error] {
	return func(yield func(events.Event, error) bool) {
		if !yield(events.NewRunStartedEvent(threadID, runID), nil) {
			return
		}
		st := textRunState{idgen: idgen}
		for ev, err := range seq {
			if err != nil {
				yield(events.NewRunErrorEvent(err.Error()), nil)
				return
			}
			if ev == nil {
				continue
			}

			// A pause closes any open text run, then ends the run with an interrupt
			// outcome and STOPS — no trailing success RUN_FINISHED.
			if ai := ev.Actions.AwaitingInput; ai != nil {
				if !st.close(yield) {
					return
				}
				yield(events.NewRunFinishedEventWithOptions(threadID, runID,
					events.WithInterruptOutcome([]types.Interrupt{interruptFrom(ai)})), nil)
				return
			}

			// Tool lifecycle interrupts an open text run first.
			if ti := ev.Actions.ToolInvocation; ti != nil {
				if !st.close(yield) {
					return
				}
				if !emitToolInvocation(yield, idgen, ti) {
					return
				}
				continue
			}

			// A tool-result PREVIEW overloads LLMResponse.Content but is identified by
			// the tool_call_id marker in StateDelta (Pitfall 2): it is a TOOL_CALL_RESULT,
			// NEVER assistant prose. Checked BEFORE the prose branch.
			if callID, ok := toolResultCallID(ev); ok {
				if !st.close(yield) {
					return
				}
				preview := ""
				if ev.LLMResponse != nil {
					preview = ev.LLMResponse.Content
				}
				if !yield(events.NewToolCallResultEvent(idgen.NewToolResultID(callID), callID, preview), nil) {
					return
				}
				continue
			}

			// A non-tool_call_id StateDelta (usage/cost/termination) interrupts the
			// text run and maps to STATE_DELTA over SORTED keys (deterministic).
			if len(ev.Actions.StateDelta) > 0 {
				if !st.close(yield) {
					return
				}
				if !yield(events.NewStateDeltaEvent(stateDeltaOps(ev.Actions.StateDelta)), nil) {
					return
				}
				continue
			}

			if ev.LLMResponse == nil {
				continue
			}
			// The final Event carries the full answer + FinishReason: close the run as
			// END-only, do NOT re-emit its Content (OQ1 policy a).
			if ev.LLMResponse.FinishReason != "" {
				if !st.close(yield) {
					return
				}
				continue
			}
			// A streamed chunk delta: open START on the first non-empty delta, CONTENT
			// per non-empty delta, skip empty deltas (Validate rejects empty deltas).
			if ev.LLMResponse.Content != "" {
				if !st.content(yield, ev.LLMResponse.Content) {
					return
				}
			}
		}
		if !st.close(yield) {
			return
		}
		yield(events.NewRunFinishedEventWithOptions(threadID, runID, events.WithSuccessOutcome()), nil)
	}
}

// textRunState coalesces a contiguous run of per-token chunk Events into ONE
// TEXT_MESSAGE_START / N*TEXT_MESSAGE_CONTENT / TEXT_MESSAGE_END lifecycle.
type textRunState struct {
	idgen IDGenerator
	msgID string
	open  bool
}

// content emits a CONTENT delta, opening the run (START) lazily on the first one.
func (s *textRunState) content(yield func(events.Event, error) bool, delta string) bool {
	if !s.open {
		s.msgID = s.idgen.NewMessageID()
		s.open = true
		if !yield(events.NewTextMessageStartEvent(s.msgID, events.WithRole("assistant")), nil) {
			return false
		}
	}
	return yield(events.NewTextMessageContentEvent(s.msgID, delta), nil)
}

// close ends an open text run (END) and resets state; a no-op when none is open.
func (s *textRunState) close(yield func(events.Event, error) bool) bool {
	if !s.open {
		return true
	}
	s.open = false
	id := s.msgID
	s.msgID = ""
	return yield(events.NewTextMessageEndEvent(id), nil)
}

// emitToolInvocation maps a ToolInvocation lifecycle Event onto AG-UI tool events:
// start → TOOL_CALL_START (+ TOOL_CALL_ARGS when Arguments != ""); end →
// TOOL_CALL_END + TOOL_CALL_RESULT.
func emitToolInvocation(yield func(events.Event, error) bool, idgen IDGenerator, ti *agent.ToolInvocation) bool {
	switch ti.Event {
	case agent.ToolInvocationStart:
		if !yield(events.NewToolCallStartEvent(ti.ToolCallID, ti.ToolName), nil) {
			return false
		}
		if ti.Arguments != "" {
			if !yield(events.NewToolCallArgsEvent(ti.ToolCallID, ti.Arguments), nil) {
				return false
			}
		}
	case agent.ToolInvocationEnd:
		if !yield(events.NewToolCallEndEvent(ti.ToolCallID), nil) {
			return false
		}
		if !yield(events.NewToolCallResultEvent(idgen.NewToolResultID(ti.ToolCallID), ti.ToolCallID, ti.ResultPreview), nil) {
			return false
		}
	}
	return true
}

// toolResultCallID reports the tool_call_id marker an Aura tool-result preview
// stamps into Actions.StateDelta (Pitfall 2). Such Events carry their text in
// LLMResponse.Content with no FinishReason — the marker is the only disambiguator
// from a streamed assistant chunk.
func toolResultCallID(ev *agent.Event) (string, bool) {
	v, ok := ev.Actions.StateDelta["tool_call_id"]
	if !ok {
		return "", false
	}
	s, _ := v.(string)
	return s, true
}

// stateDeltaOps builds a JSONPatchOperation slice over SORTED keys so a multi-key
// StateDelta serializes deterministically (map iteration order is non-deterministic
// — unstable golden compares otherwise).
func stateDeltaOps(d map[string]any) []events.JSONPatchOperation {
	keys := make([]string, 0, len(d))
	for k := range d {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ops := make([]events.JSONPatchOperation, 0, len(keys))
	for _, k := range keys {
		ops = append(ops, events.JSONPatchOperation{Op: "replace", Path: "/" + k, Value: d[k]})
	}
	return ops
}

// interruptFrom maps an AwaitingInput pause payload onto an AG-UI Interrupt, with
// a JSON-Schema ResponseSchema describing the expected resume answer.
func interruptFrom(ai *agent.AwaitingInput) types.Interrupt {
	return types.Interrupt{
		ID:             ai.ToolCallID,
		Reason:         ai.Kind,
		Message:        ai.Question,
		ToolCallID:     ai.ToolCallID,
		ResponseSchema: responseSchema(ai.Options),
	}
}

// responseSchema builds the JSON-Schema object for the resume payload. When the
// pause offered discrete options, the answer is constrained to their values; a
// free-form pause accepts any string.
func responseSchema(options []agent.PauseOption) map[string]any {
	answer := map[string]any{"type": "string"}
	if len(options) > 0 {
		enum := make([]any, len(options))
		for i, o := range options {
			enum[i] = o.Value
		}
		answer["enum"] = enum
	}
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"answer": answer},
	}
}
