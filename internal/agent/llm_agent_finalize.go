// Forced finalization (Req#2, the SPINE for plan 04's recovery/fallback work):
// when the run-loop trips an early-termination gate (budget max_steps/wallclock,
// or the dedup veto) it used to die on a prose-less terminalBudgetEvent — the
// ~1-in-6 empty answer. finalize() instead issues ONE tool-free synthesis turn
// (ToolChoice="none", full in-memory history + the D-03 nudge), drains it to a
// content string, and emits a NON-EMPTY finalEvent that still carries the trip
// reason in StateDelta for observability (landmine #10).
//
// Concern-split out of llm_agent.go (D-07): that file is at its no-god-class
// headroom, and the recovery counter + Italian-stub fallback land here in plan 04.
package agent

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/llm"
)

// finalizeNudge is the D-03 synthesis instruction appended (as a trailing user
// message on a COPY of history) to the tool-free finalize turn. English, NO
// Italian directive — SystemPrompt governs output language (D-01). Load-bearing
// literal: a test asserts the request carries it.
const finalizeNudge = "Stop calling tools. Using only the tool results already gathered above, " +
	"write the final answer to the user's original question now."

// recoveryNudgeTool / recoveryNudgeGeneric are the D-02 recovery nudges injected
// EXACTLY ONCE on the first early-termination trip (user role, appended to the REAL
// a.history TAIL — never messages[0]). The dedup path interpolates the offending
// tool name; the pure-budget path (max_steps/wallclock at the :124 gate has no
// offending tool — landmine #9) uses the generic variant. English, no Italian
// directive (D-01). Both keep the "do not repeat / answer now from gathered
// results" intent.
const (
	recoveryNudgeToolPrefix = "You have already called `"
	recoveryNudgeToolSuffix = "` with these arguments and the result will not change. " +
		"Do not repeat any tool call — answer the user's question now using the results you have already gathered."
	recoveryNudgeGeneric = "You have run out of tool-call budget and may not call any more tools. " +
		"Do not repeat any tool call — answer the user's question now using the results you have already gathered."
)

// maybeRecover is the single counter-gated recovery decision used by BOTH trip
// sites (D-08). On the FIRST trip (recoveryAttempts==0) it increments the counter,
// appends ONE user-role nudge to the real a.history tail (the tool variant when
// toolName != "", the generic variant for the pure-budget path — landmine #9), and
// returns true so the loop runs one more turn. On any SUBSEQUENT trip it returns
// false, signalling the caller to finalize(). It is a COUNTER, never a one-shot
// boolean latch (CrewAI #1656 anti-pattern). The nudge is REAL conversation state
// (unlike the <budget> block, which tail-injects to a COPY — landmine #1).
func (a *LlmAgent) maybeRecover(toolName string) (recovered bool) {
	if a.recoveryAttempts >= 1 {
		return false
	}
	a.recoveryAttempts++
	nudge := recoveryNudgeGeneric
	if toolName != "" {
		nudge = recoveryNudgeToolPrefix + toolName + recoveryNudgeToolSuffix
	}
	a.history = append(a.history, llm.Message{Role: llm.RoleUser, Content: nudge})
	return true
}

// recoveryNudgeEmpty is the empty-response recovery variant: the provider returned
// a completion with no tool calls AND no content (a hiccup, not an answer). The
// nudge sends the model back to the task; it shares the recoveryAttempts counter
// with the budget/dedup nudges so the combined recovery ceiling stays ONE turn.
const recoveryNudgeEmpty = "Your last response was empty. Continue the task now — " +
	"call the next tool you need, or deliver the final answer via text_response."

// maybeRecoverEmptyResponse mirrors maybeRecover for the empty-completion case
// (llm_agent.go natural-stop branch): first occurrence appends the empty-response
// nudge and returns true for one more turn; any subsequent occurrence returns false
// so the caller finalizes. Counter shared with maybeRecover — never a loop.
func (a *LlmAgent) maybeRecoverEmptyResponse() bool {
	if a.recoveryAttempts >= 1 {
		return false
	}
	a.recoveryAttempts++
	a.history = append(a.history, llm.Message{Role: llm.RoleUser, Content: recoveryNudgeEmpty})
	return true
}

// stubLeadIn is the D-09 deterministic-stub lead-in: emitted (in Italian, no LLM
// call) when synthesis returns empty/errors TWICE so the user always receives the
// gathered data. Load-bearing literal — a test asserts the terminal content starts
// with it.
const stubLeadIn = "Non sono riuscito a completare la sintesi finale, ma ecco i dati che ho raccolto:"

// stub byte caps (D-09, Claude's discretion): each tool-result preview is truncated
// to stubBulletCap bytes and the whole digest is capped at stubDigestCap bytes so a
// runaway tool result cannot inflate the terminal Event unboundedly (T-07.1-07).
const (
	stubBulletCap = 500
	stubDigestCap = 8000
)

// finalize issues the forced-finalization synthesis turn for an early-termination
// trip (reason in max_steps / wallclock / dedup) and emits the terminal Event. It
// builds a tool-free request (ToolChoice="none") from a COPY of history plus the
// D-03 nudge, opens through the shared stream retry helper, and never spends
// another budget step (Req#4 invariant; the bounded ceiling is enforced and tested by
// TestFinalizeOutsideBudget). It RETRIES the synthesis ONCE when the first attempt
// is empty/errors (D-09). When BOTH attempts fail it falls back to a deterministic
// Italian stub digesting the gathered RoleTool results (no LLM call) so NO path ever
// emits empty final prose. The answer is appended to history (mirroring the
// content-stop path) and yielded as ONE finalEvent whose StateDelta carries the
// usage delta AND the trip reason keys. The terminal user-facing Event is ALWAYS a
// non-empty finalEvent, never the iter.Seq2 error slot (L2/D-04).
// acc carries the usage of the calls the turn already made. The synthesis call below is
// one MORE billed call, so it is folded in rather than reported alone: finalize is the
// budget-trip path, i.e. precisely the turns that made the most calls before arriving
// here, and reporting only the synthesis would understate them the most.
func (a *LlmAgent) finalize(ic InvocationContext, spanID [8]byte, parentSpanID *[8]byte,
	requestID, reason string, acc *turnUsage, yield func(*Event, error) bool,
) {
	answer, usage := a.synthesizeWithFallback(ic)
	acc.add(usage)
	a.history = append(a.history, llm.Message{Role: llm.RoleAssistant, Content: answer})
	yield(a.finalizeEvent(ic, spanID, parentSpanID, requestID, answer, reason, acc.total()), nil)
}

// synthesizeWithFallback runs the tool-free synthesis call, retrying it EXACTLY ONCE
// when the first attempt returns empty content or a transport error (D-09). If the
// retry yields non-empty prose that prose is returned; if BOTH attempts are
// empty/error it returns the deterministic Italian stub digest (NO LLM call). The
// transport error is consumed here (logged via %w inside synthesize, never bulleted
// into user prose — T-07.1-07); the caller always receives non-empty content.
func (a *LlmAgent) synthesizeWithFallback(ic InvocationContext) (answer string, usage llm.Usage) {
	answer, usage, err := a.synthesize(ic)
	if err == nil && answer != "" {
		return answer, usage
	}
	retryAnswer, retryUsage, retryErr := a.synthesize(ic)
	if retryErr == nil && retryAnswer != "" {
		return retryAnswer, retryUsage
	}
	return a.stubDigest(), usage
}

// stubDigest builds the D-09 deterministic Italian fallback: the lead-in followed by
// one `- {tool}: {preview}` bullet per RoleTool result in a.history. The tool name is
// correlated from the preceding assistant tool-call messages (RoleTool carries only
// the ToolCallID + preview). Each preview is truncated to stubBulletCap and the whole
// digest to stubDigestCap. No LLM call (D-05); always non-empty (the lead-in alone
// guarantees it even with zero tool results).
func (a *LlmAgent) stubDigest() string {
	names := toolNamesByCallID(a.history)
	var b strings.Builder
	b.WriteString(stubLeadIn)
	for i := range a.history {
		m := a.history[i]
		if m.Role != llm.RoleTool {
			continue
		}
		if b.Len() >= stubDigestCap {
			break
		}
		name := names[m.ToolCallID]
		if name == "" {
			name = "tool"
		}
		b.WriteString("\n- ")
		b.WriteString(name)
		b.WriteString(": ")
		b.WriteString(truncateBytes(m.Content, stubBulletCap))
	}
	return truncateBytes(b.String(), stubDigestCap)
}

// toolNamesByCallID maps each tool_call_id to its tool name by scanning the
// assistant tool-call messages, so the stub can label each RoleTool preview (which
// carries only the id).
func toolNamesByCallID(history []llm.Message) map[string]string {
	names := make(map[string]string)
	for i := range history {
		for _, tc := range history[i].ToolCalls {
			names[tc.ID] = tc.Function.Name
		}
	}
	return names
}

// truncateBytes clamps s to at most n bytes on a valid UTF-8 boundary (the previews
// are UTF-8 prose), avoiding a split rune in the user-facing digest.
func truncateBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// synthesize runs the tool-free synthesis call and returns the content-parsed
// answer plus the trailing usage. It NEVER spends a budget step — the finalize turn
// rides outside the step gate (Req#4; the bounded ceiling is enforced and tested by
// TestFinalizeOutsideBudget). On a transport error it wraps with %w so the caller
// (synthesizeWithFallback) can branch to the retry/stub path; the error is consumed
// there and never surfaces as user-facing prose.
func (a *LlmAgent) synthesize(ic InvocationContext) (answer string, usage llm.Usage, err error) {
	finalizeHistory := append(append([]llm.Message(nil), a.history...),
		llm.Message{Role: llm.RoleUser, Content: finalizeNudge})
	req := a.builder.Build(finalizeHistory, a.registry, a.cfg.Provider, a.cfg, prompt.Budget{}, a.activated)
	req.ToolChoice = "none"
	req.SessionID = a.sessionID

	// D-19 total-timeout on the ctx, mirroring the main loop (llm_agent.go:251).
	// Production (internal/runner/runner.go) drives ic.Ctx from budget.WithDeadline —
	// an ABSOLUTE deadline equal to the SAME wallclock cutoff ConsumeStep checks
	// (budget.go). finalize is reached PRECISELY when that budget trips (max_steps,
	// wallclock, dedup, or the recovery counter already spent), so on the wallclock
	// path ic.Ctx's deadline has ALREADY passed at this point. Deriving
	// context.WithTimeout straight off ic.Ctx would inherit that already-Done parent
	// (WithDeadline collapses to WithCancel when the parent deadline is earlier, and
	// an already-canceled parent cancels the child synchronously) — the salvage call
	// would be dead-on-arrival, and the deterministic stub fallback would never even
	// be reached because BOTH synthesis attempts fail instantly (fix-plan 1.1,
	// CONFIRMED high). context.WithoutCancel severs the expired deadline (keeping any
	// request-scoped values) so this call gets its own FRESH TotalTimeoutSec window.
	// The derived ctx is cancelled only after the channel is fully drained below, so
	// partial chunks are never dropped.
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ic.Ctx), time.Duration(a.cfg.TotalTimeoutSec)*time.Second)
	defer cancel()
	callCtx, llmEnd := llmCallBoundary.Start(callCtx)
	defer llmEnd.PanicSafe(&err)

	ch, serr := a.streamWithOpenRetry(callCtx, req, ic.RequestID.String()+":finalize")
	if serr != nil {
		recordLLMError(llmErrorKind("finalize_open", serr))
		return "", llm.Usage{}, fmt.Errorf("finalize synthesis stream: %w", serr)
	}

	var b strings.Builder
	for c := range ch {
		switch {
		case c.Err != nil:
			recordLLMError(llmErrorKind("finalize_stream", c.Err))
			return "", usage, fmt.Errorf("finalize synthesis stream: %w", c.Err)
		case c.Usage != nil:
			usage = *c.Usage
			recordUsage(usage)
		case c.Text != "":
			b.WriteString(c.Text)
		}
	}
	return b.String(), usage, nil
}

// finalizeEvent is the terminal Event for a forced-finalization turn: the
// content-parsed synthesized answer with the usage footer, AUGMENTED with the
// trip reason (termination_reason/limit_hit) so observability survives the
// non-empty-prose terminal (landmine #10 — finalEvent alone does not carry these).
// The reason keys are sourced from terminalBudgetEvent (the single owner of the
// D-04 termination StateDelta shape) rather than re-spelling them here, so the two
// paths stay byte-consistent: terminalBudgetEvent is no longer the terminal Event
// at the trip sites, but its StateDelta keys are preserved on the finalEvent.
func (a *LlmAgent) finalizeEvent(ic InvocationContext, spanID [8]byte, parentSpanID *[8]byte,
	requestID, answer, reason string, usage llm.Usage,
) *Event {
	ev := a.finalEvent(ic, spanID, parentSpanID, requestID, answer, "stop", usage)
	maps.Copy(ev.Actions.StateDelta, a.terminalBudgetEvent(ic, spanID, parentSpanID, reason).Actions.StateDelta)
	return ev
}
