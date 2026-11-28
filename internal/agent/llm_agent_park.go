package agent

import (
	"slices"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

// dedupSuppressed answers every call of a batch the dedup guard stopped before any ran.
const dedupSuppressed = "skipped: duplicate call suppressed by the dedup guard"

// endOnPause ends the round when pause detection found anything (step 4 of Run). Pass 1
// rewrites the assistant message to its ask_user calls and drops the siblings (D-A1-07);
// pass 2's pauses end the round in a park round.
func (a *LlmAgent) endOnPause(ic InvocationContext, spanID [8]byte, requestID string,
	calls []llm.ToolCall, acc *turnUsage, yield func(*Event, error) bool,
) (paused bool, infraErr error) {
	pauses, askUser := a.pauseCalls(ic.Ctx, calls)
	switch {
	case len(pauses) == 0:
		return false, nil
	case askUser:
		a.history = append(a.history, llm.Message{Role: llm.RoleAssistant, ToolCalls: pauseToolCalls(pauses)})
		a.emitPauses(ic, spanID, pauses, yield)
		return true, nil
	default:
		return true, a.parkRound(ic, spanID, requestID, calls, pauses, acc, yield)
	}
}

// parkRound ends a round in which pass 2 parked at least one call (D19, AM-Siblings). The
// calls that need nobody run now, as in every industrial runtime, instead of costing a model
// round to be emitted again:
//  1. the terminal text_response is dropped, and the assistant message keeps every other call;
//  2. one ParkedTurn event carries those calls and every pause, so the Runner commits the
//     parked turn and its pause rows before any sibling runs;
//  3. the unparked calls dispatch in the model's order, with no terminal among them, so
//     nothing answers ahead of the pauses; in a batch that runs in order, only those before
//     the first parked call run, and the rest are answered as not run (parkRoundSiblings);
//  4. the pause events close the stream, back to back.
//
// It returns before Run's post-dispatch steer drain: a steer waits for the next turn rather
// than riding a sibling's result. A consumer stop ends the round where it stopped; the
// pauses are already in ParkedTurn.
func (a *LlmAgent) parkRound(ic InvocationContext, spanID [8]byte, requestID string,
	calls []llm.ToolCall, pauses []pauseCall, acc *turnUsage, yield func(*Event, error) bool,
) (infraErr error) {
	kept := slices.DeleteFunc(slices.Clone(calls), func(c llm.ToolCall) bool { return c.Function.Name == terminalTool })
	a.history = append(a.history, llm.Message{Role: llm.RoleAssistant, ToolCalls: kept})

	pauseEvents := make([]*Event, len(pauses))
	turn := &ParkedTurn{Calls: slices.Clone(kept), Pauses: make([]*AwaitingInput, len(pauses))}
	parked := make(map[string]bool, len(pauses))
	for i, p := range pauses {
		pauseEvents[i] = a.pauseCallEvent(ic, spanID, p)
		turn.Pauses[i] = pauseEvents[i].Actions.AwaitingInput
		parked[p.call.ID] = true
	}
	opened := a.newEvent(ic, spanID)
	opened.Actions.ParkedTurn = turn
	if !yield(opened, nil) {
		return nil
	}

	siblings, held := a.parkRoundSiblings(kept, parked)
	if len(siblings) > 0 {
		stopped, err := a.dispatchCalls(ic, spanID, requestID, siblings, acc, yield, true)
		if err != nil || stopped {
			return err
		}
	}
	if len(held) > 0 {
		notRun := heldBehindAParkedCall(pauses[0].call.Function.Name)
		a.appendSyntheticToolResults(held, notRun)
		if !a.yieldSyntheticResults(ic, spanID, held, notRun, yield) {
			return nil
		}
	}
	for _, ev := range pauseEvents {
		if !yield(ev, nil) {
			return nil
		}
	}
	return nil
}

// parkRoundSiblings splits a park round's unparked calls into those that run now and those
// held. Siblings independentBatch would run at once keep no order, so they all run (a read
// after the parked call reads before it: D19's accepted limit). Any other batch is a
// sequence, the order executeBatch keeps since 2026-10-19 (a save, then the send_file that
// delivers it): a sibling after the first parked call would run before it, and the accept
// would then act on what that sibling changed, so only the siblings before the first parked
// call run. The cost is one model round to emit the held calls again.
func (a *LlmAgent) parkRoundSiblings(kept []llm.ToolCall, parked map[string]bool) (run, held []llm.ToolCall) {
	unparked := slices.DeleteFunc(slices.Clone(kept), func(c llm.ToolCall) bool { return parked[c.ID] })
	if a.independentBatch(unparked) {
		return unparked, nil
	}
	pastPark := false
	for _, call := range kept {
		switch {
		case parked[call.ID]:
			pastPark = true
		case pastPark:
			held = append(held, call)
		default:
			run = append(run, call)
		}
	}
	return run, held
}

// heldBehindAParkedCall answers a call parkRoundSiblings held.
func heldBehindAParkedCall(parkedTool string) string {
	return "Not run: this call comes after " + parkedTool + " in a batch that runs in order, and that call " +
		"is waiting for the operator's decision. Call it again once the decision is known."
}

// yieldSyntheticResults streams the synthetic answer of each call as its result event, for a
// batch that ends before running anything. It reports false once the consumer stops.
func (a *LlmAgent) yieldSyntheticResults(ic InvocationContext, spanID [8]byte,
	calls []llm.ToolCall, content string, yield func(*Event, error) bool,
) bool {
	now := time.Now().UTC()
	for _, call := range calls {
		if call.ID == "" {
			continue
		}
		run := toolRunResult{
			ToolCallID: call.ID, ToolName: call.Function.Name, Arguments: call.Function.Arguments,
			StartedAt: now, EndedAt: now, Preview: content,
			Result: tools.ToolResult{Preview: content, Bytes: len(content)},
		}
		if !yield(a.toolResultEvent(ic, spanID, run), nil) {
			return false
		}
	}
	return true
}
