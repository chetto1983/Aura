package agent

import (
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/canonicaljson"
	"github.com/chetto1983/aura/internal/llm"
)

// dispatch runs the assistant's tool calls for one turn (D-14, P1 parallel).
// Runnable non-terminal calls execute concurrently; shared state changes stay
// serial and in original call order so the wire contract and cache stability hold.
func (a *LlmAgent) dispatch(ic InvocationContext, spanID [8]byte, parentSpanID *[8]byte, requestID string,
	calls []llm.ToolCall, usage llm.Usage, yield func(*Event, error) bool,
) (done bool, infraErr error) {
	// Partition: the FIRST text_response is the terminal; the rest are runnable.
	terminalIdx := -1
	runnable := make([]int, 0, len(calls))
	for i := range calls {
		if calls[i].Function.Name == terminalTool {
			if terminalIdx < 0 {
				terminalIdx = i
			}
			continue
		}
		runnable = append(runnable, i)
	}

	// Hook rewrites and dedup gate (D-14), serial and in order so the ring observes
	// the same fingerprint sequence as the old sequential loop. Rewrites happen
	// before dedup/audit/execution so every downstream contract sees one call shape.
	// A trip forces recovery-or-finalization like the budget trip; nothing in this
	// batch has executed yet, so every call gets a synthetic result, keeping the
	// wire valid before the retry/finalize.
	canon := make([][]byte, len(calls))
	vetoResults := make(map[int]tools.ToolResult)
	for _, i := range runnable {
		call := calls[i]
		if res, err := a.hooks.BeforeTool(ic.Ctx, call); err != nil {
			return false, err
		} else if res != nil {
			if res.Call != nil {
				call = *res.Call
				calls[i] = call
			}
			if res.Result != nil {
				vetoResults[i] = *res.Result
			}
		}
		c := canonicaljson.CanonicalArgs(call.Function.Arguments)
		canon[i] = c
		if dedup, reason := ic.Budget.BeforeToolCall(call.Function.Name, c); dedup {
			a.appendSyntheticToolResults(calls, "skipped: duplicate call suppressed by the dedup guard")
			if a.maybeRecover(call.Function.Name) {
				return false, nil
			}
			a.finalize(ic, spanID, parentSpanID, requestID, reason, yield)
			return true, nil
		}
	}

	// Execute the runnable calls (concurrently when >1) then process their results
	// serially in original call order: start events first, run, then per-call history
	// append + AfterToolResult + result event.
	if len(runnable) > 0 {
		recordToolDispatch(len(runnable))
		startedAt := time.Now().UTC()
		for batchIndex, i := range runnable {
			if !yield(a.toolStartEvent(ic, spanID, parentSpanID, calls[i], startedAt, batchIndex, len(runnable)), nil) {
				return true, nil
			}
		}
		runsByIndex := make(map[int]toolRunResult, len(runnable))
		batch := make([]llm.ToolCall, 0, len(runnable))
		execIndices := make([]int, 0, len(runnable))
		for _, i := range runnable {
			if res, ok := vetoResults[i]; ok {
				runsByIndex[i] = hookToolRunResult(calls[i], startedAt, res)
				continue
			}
			execIndices = append(execIndices, i)
			batch = append(batch, calls[i])
		}
		runs := a.executeBatch(ic.Ctx, ic.Budget, batch, startedAt)
		for k, i := range execIndices {
			runsByIndex[i] = runs[k]
		}
		for _, i := range runnable {
			run := runsByIndex[i]
			if res, err := a.hooks.AfterTool(ic.Ctx, calls[i], run.Result); err != nil {
				return false, err
			} else if res != nil && res.Result != nil {
				run.Result = *res.Result
				run.Preview = renderToolResultForPrompt(calls[i].Function.Name, run.Result)
				if run.Result.Bytes == 0 {
					run.Result.Bytes = len(run.Result.Preview)
				}
				run.EndedAt = time.Now().UTC()
			}
			if run.Mutating {
				a.sideEffected = true // D-43: this turn touched host state; arm the completion gate.
			}
			a.history = append(a.history, llm.Message{Role: llm.RoleTool, ToolCallID: calls[i].ID, Content: run.Preview})
			// Dedup hashes the RAW result preview, not run.Preview: the prompt-facing
			// rendering wraps untrusted output in a per-call random nonce envelope
			// (AG-052), which would make every identical result look different and
			// defeat the progress veto. The underlying tool output is the stable signal.
			ic.Budget.AfterToolResult(calls[i].Function.Name, canon[i], []byte(run.Result.Preview))
			if !yield(a.toolResultEvent(ic, spanID, parentSpanID, run), nil) {
				return true, nil
			}
		}
	}

	// Terminal: text_response ends the turn after the batch has run (D-13).
	if terminalIdx >= 0 {
		return a.runTerminal(ic, spanID, parentSpanID, requestID, calls[terminalIdx], usage, yield), nil
	}
	return false, nil
}
