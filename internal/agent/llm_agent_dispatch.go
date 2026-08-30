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
	calls []llm.ToolCall, acc *turnUsage, yield func(*Event, error) bool,
) (done bool, infraErr error) {
	// Partition: the FIRST text_response is the terminal; the rest are runnable.
	// terminalCount also tallies any EXTRA text_response calls so a double-terminal
	// step can be rejected below (a 2nd text_response was silently dropped before).
	terminalIdx := -1
	terminalCount := 0
	runnable := make([]int, 0, len(calls))
	for i := range calls {
		if calls[i].Function.Name == terminalTool {
			terminalCount++
			if terminalIdx < 0 {
				terminalIdx = i
			}
			continue
		}
		runnable = append(runnable, i)
	}

	// LOOP-01/F-003 (D-01): a terminal text_response is a FINAL answer, and native
	// tool-use semantics say any tool call present in the step means the turn is NOT
	// final. So a text_response alongside ANY runnable sibling OR a second
	// text_response is the untrusted shape F-003 exploited — reject the whole step
	// before any sibling hook/dedup/Execute runs and force a replan-or-finalize,
	// reusing the dedup-trip control flow. This is deliberately classification-
	// INDEPENDENT: the Mutating flag is untrustworthy while skill/task/swarm_spawn
	// stay unflagged (a Phase-35 classification-hardening gap), so "allow read-only
	// siblings" is unsafe — the whole mixed step is rejected (not option B).
	// appendSyntheticToolResults appends a RoleTool for every call id (the terminal
	// included) so the wire stays valid before the retry/finalize, which also closes
	// the latent double-terminal drop (D-01b).
	if terminalIdx >= 0 && (len(runnable) > 0 || terminalCount > 1) {
		a.appendSyntheticToolResults(calls, "rejected: a terminal text_response cannot be combined with other tool calls or a second text_response; replan with a single final answer")
		if a.maybeRecover(terminalTool) {
			return false, nil
		}
		a.finalize(ic, spanID, parentSpanID, requestID, "terminal_exclusivity", acc, yield)
		return true, nil
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
			a.finalize(ic, spanID, parentSpanID, requestID, reason, acc, yield)
			return true, nil
		}
	}

	// Execute the runnable calls (concurrently when >1) then process their results
	// serially in original call order: start events first, run, then per-call history
	// append + AfterToolResult + result event.
	if len(runnable) > 0 {
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
				// The verify-on-stop gate needs to know WHAT was edited, not just that
				// something was. It rides the SAME condition deliberately: a call vetoed
				// by a BeforeTool policy hook, refused as an unloaded deferred tool, or
				// dispatched to an unknown name never reaches Execute and never sets
				// Mutating, so it must not leave an edited path behind — the gate would
				// then spend up to two model rounds demanding proof of a write that
				// policy blocked. A write that RAN and failed still counts: runTool sets
				// Mutating before execTool, and a failed write did touch the workspace.
				a.recordEditedPath(calls[i])
			}
			// Full-promotion parity: if this run was a tool_search hit, promote the tools
			// it loaded (MetaActivatedTools) into a.activated so the NEXT turn's buildRequest
			// makes them callable WITH their schema. Race-free: this is the serial result
			// loop (executeBatch already joined); the concurrent batch only reads a.activated.
			a.promoteFromMeta(run.Result.Meta)
			historyMessage := llm.Message{Role: llm.RoleTool, ToolCallID: calls[i].ID, Content: run.Preview}
			if err := a.hooks.AfterToolHistory(ic.Ctx, calls[i], historyMessage); err != nil {
				return false, err
			}
			a.history = append(a.history, historyMessage)
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
	if terminalIdx >= 0 && terminalIdx < len(calls) {
		terminalCall := calls[terminalIdx]
		return a.runTerminal(ic, spanID, parentSpanID, requestID, terminalCall, acc, yield), nil
	}
	return false, nil
}
