// Pause-DETECTION seam (D-A1-03 / AM-01). This file holds the half of the
// HITL pause primitive that lives in the agent: catching the
// tools.ErrAwaitingUserInput sentinel, applying intra-turn exclusivity, and
// emitting the pause as an Actions.AwaitingInput Event. The agent stays DB-free —
// pause PERSISTENCE + resume orchestration live in the Runner (04-05), which
// observes this Event and is the sole writer of aura.paused_states. Split out of
// llm_agent.go for separation of concern (AM-01), not size.
//
// The pause is Event-only, NEVER the iter.Seq2 error slot (RESEARCH Pattern-3
// anti-pattern; mirrors budget exhaustion at llm_agent.go terminalBudgetEvent).
//
// T-04-19 WIDENING (Phase-29 D-13, Option A): the Runner is no longer the SOLE
// writer of aura.paused_states. The capability-gated operator-origin
// governance-write path (a cockpit skill install behind
// RequireCapability(governance.write)) mints an operator-origin ask_user pause via
// askuser.Store.Insert. That widening lives ENTIRELY outside the agent: the agent
// path here stays name-gated to ask_user (only an ask_user tool call mints a pause)
// and DB-free — no model/agent/unauthenticated caller can mint a pause through this
// file. The second writer is scoped to the operator capability gate, not a blanket
// relaxation of this name-gate.
package agent

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/obs"
)

// askUserToolName is the canonical ask_user tool name, read from its Spec so a
// rename of the tool cannot silently break pause detection.
var askUserToolName = tools.AskUser{}.Spec().Name

var pauseBoundary = obs.NewGlobalBoundary(agentMeterName, obs.BoundaryConfig{
	Operation: "pause_create", State: "pending", Count: obs.AgentPauseTransitionsID,
})

// pauseCalls scans the assistant's batched tool calls and returns their pauses in
// the model's order, with each call's ToolCallID stamped onto the carried sentinel, and
// whether pass 1 (ask_user) produced them.
//
// Pass 1 is ask_user, the only tool pre-executed: when it pauses, its calls rewrite the
// assistant message (D-A1-07 OpenAI wire-correctness), and the siblings are dropped and
// re-emitted by the model on the next round. A malformed ask_user that fails validation
// is NOT a pause — it returns no sentinel and falls through to normal dispatch as a
// RoleTool error. Pass 2 (withheldCalls) runs only when pass 1 found nothing: it parks the
// calls the host withholds without running them, and its siblings run in a park round
// (llm_agent_park.go), as far as the batch's order allows.
//
// ARCHITECTURAL CONSTRAINT (amendment #51 / D-40): ask_user alone can pause by
// returning its sentinel. A host-owned trusted outbound message may also pause
// through withholdOutboundMessage, a tools.HostReviewer call (a scheduled message) through
// withholdHostReviewedCall, and a call the gateway would ask about through
// withholdForApproval; none calls Execute. A non-ask_user tool whose Execute returns
// tools.ErrAwaitingUserInput does NOT pause; runTool renders it as a RoleTool error
// (`error: awaiting user input`, the sentinel's Error() string) and the loop
// continues. Spike 012b proved this: the old skill install-gate returned the
// sentinel from a non-ask_user tool, its red-flag/question payload was silently
// dropped, and the turn never paused. That dead install-gate path was deleted in
// plan 11-09 Task 1. Any future tool needing to pause MUST route through ask_user
// (or the dispatch must learn to surface tool sentinels) — never assume a tool
// error pauses. Proven by TestAskUserOnlyPauseConstraint.
func (a *LlmAgent) pauseCalls(ctx context.Context, calls []llm.ToolCall) (pauses []pauseCall, askUser bool) {
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
	if len(out) > 0 {
		return out, true
	}
	return a.withheldCalls(ctx, calls), false
}

// pauseCall pairs a finalized tool call with its decoded pause sentinel. A withheld call
// also carries its original name and arguments, and a gated one what it may run on accept.
type pauseCall struct {
	call              llm.ToolCall
	pause             *tools.ErrAwaitingUserInput
	originalToolName  string
	originalArguments string
	approval          *ParkedApproval
}

// pauseToolCalls projects pass 1's ask_user pauses back to the ask_user-only
// []llm.ToolCall that rewrites the assistant message (D-A1-07 wire-correctness). A park
// round keeps every call instead.
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
	if pause, ok := errors.AsType[*tools.ErrAwaitingUserInput](err); ok {
		_, end := pauseBoundary.Start(ctx)
		end.End(nil)
		return pause, true
	}
	return nil, false
}

// emitPauses yields one pause Event per detected call, in the order the model emitted
// them. The caller has already written the assistant message; NO RoleTool result is
// appended for a pause (the loop is suspended, the Runner injects the answer on resume).
// The yield-after-false guard is honored: once the consumer returns false the loop
// stops yielding (iter.Seq2 contract).
func (a *LlmAgent) emitPauses(ic InvocationContext, spanID [8]byte,
	pauses []pauseCall, yield func(*Event, error) bool,
) {
	for _, p := range pauses {
		if !yield(a.pauseCallEvent(ic, spanID, p), nil) {
			return
		}
	}
}

// pauseCallEvent is a detected call's pause Event, with what only the Runner reads.
func (a *LlmAgent) pauseCallEvent(ic InvocationContext, spanID [8]byte, p pauseCall) *Event {
	event := a.pauseEvent(ic, spanID, p.pause)
	event.Actions.AwaitingInput.OriginalToolName = p.originalToolName
	event.Actions.AwaitingInput.OriginalArguments = p.originalArguments
	event.Actions.AwaitingInput.Approval = p.approval
	return event
}

// pauseEvent builds the HITL pause Event carrying Actions.AwaitingInput (D-A1-03).
// It projects the tools.ErrAwaitingUserInput payload onto the Event model's
// AwaitingInput (no DB type crosses here) and stamps OriginAgent for swarm
// forward-compat (D-A1-08).
func (a *LlmAgent) pauseEvent(ic InvocationContext, spanID [8]byte,
	pause *tools.ErrAwaitingUserInput,
) *Event {
	ev := a.newEvent(ic, spanID)
	ev.Actions.AwaitingInput = &AwaitingInput{
		Question:           pause.Question,
		Options:            pauseOptions(pause.Options),
		Kind:               pause.Kind,
		Priority:           pause.Priority,
		ToolCallID:         pause.ToolCallID,
		ResumeContext:      pause.ResumeContext,
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
