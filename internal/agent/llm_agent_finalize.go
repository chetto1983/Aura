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
	"fmt"
	"strings"

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

// finalize issues the forced-finalization synthesis turn for an early-termination
// trip (reason in max_steps / wallclock / dedup) and emits the terminal Event. It
// builds a tool-free request (ToolChoice="none") from a COPY of history plus the
// D-03 nudge, calls a.client.Stream DIRECTLY — it never spends another budget step
// (Req#4 invariant; the bounded ceiling is enforced and tested in plan 04) — drains
// it to a content string, appends the answer to history (mirroring the content-stop
// path), and yields ONE finalEvent whose StateDelta carries the usage delta AND the
// trip reason keys. The terminal user-facing Event is ALWAYS a finalEvent — on a
// real transport error this still emits a finalEvent (empty content this plan; plan
// 04 inserts the retry-once + Italian stub), never the iter.Seq2 error slot (L2/D-04).
func (a *LlmAgent) finalize(ic InvocationContext, spanID [8]byte, parentSpanID *[8]byte,
	requestID, reason string, yield func(*Event, error) bool,
) {
	answer, usage, _ := a.synthesize(ic)
	a.history = append(a.history, llm.Message{Role: llm.RoleAssistant, Content: answer})
	yield(a.finalizeEvent(ic, spanID, parentSpanID, requestID, answer, reason, usage), nil)
}

// synthesize runs the tool-free synthesis call and returns the content-parsed
// answer plus the trailing usage. It NEVER spends a budget step — the finalize turn
// rides outside the step gate (Req#4; the bounded ceiling is enforced and tested in
// plan 04). On a transport error it wraps with %w so plan 04 can branch on it; this
// plan's finalize ignores the error and still emits a (possibly empty) finalEvent
// for the happy path.
func (a *LlmAgent) synthesize(ic InvocationContext) (answer string, usage llm.Usage, err error) {
	finalizeHistory := append(append([]llm.Message(nil), a.history...),
		llm.Message{Role: llm.RoleUser, Content: finalizeNudge})
	req := a.builder.Build(finalizeHistory, a.registry, a.cfg.Provider, a.cfg, prompt.Budget{})
	req.ToolChoice = "none"
	req.SessionID = a.sessionID

	ch, serr := a.client.Stream(ic.Ctx, req)
	if serr != nil {
		return "", llm.Usage{}, fmt.Errorf("finalize synthesis stream: %w", serr)
	}

	var b strings.Builder
	for c := range ch {
		switch {
		case c.Usage != nil:
			usage = *c.Usage
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
	for k, v := range a.terminalBudgetEvent(ic, spanID, parentSpanID, reason).Actions.StateDelta {
		ev.Actions.StateDelta[k] = v
	}
	return ev
}
