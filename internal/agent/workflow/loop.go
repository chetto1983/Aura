// Pattern derivato da google/adk-go v1.4.0 agent/workflowagents/loopagent/agent.go
// (Apache 2.0). Adattato per Aura con SC#2 budget exhaustion + SC#3
// child-inherits-remaining + SC#4 UUIDv7 OTel-compat.
//
// Aura divergences from adk's LoopAgent control flow:
//   - adk LoopAgent has NO budget; Aura threads ic.Budget through every step
//     (ConsumeStep per tool-call, D-11) so a runaway sub is bounded (SC#2).
//   - termination on budget exhaustion is signalled by an EXPLICIT Event
//     (Actions.Escalate + StateDelta), NEVER through the iter error slot (D-04).
//   - two-phase dedup: BeforeToolCall blocks a repeating tool call before its side
//     effect re-runs, AfterToolResult records a bounded result preview as a
//     progress veto (D-18); the CALLER canonicalizes args (B2).
//   - subs run under ic.WithSubAgent (the SAME dedup ring across iterations, D-09)
//     so cross-iteration repeats are visible; Branch gains .iter-<N> per pass (D-15).
package workflow

import (
	"encoding/json"
	"iter"
	"strconv"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/canonicaljson"
	"github.com/chetto1983/aura/internal/llm"
)

// LoopAgent re-runs its sub-agents until one of three terminal conditions fires:
// (a) maxIterations passes completed, (b) a sub emits Actions.Escalate=true, or
// (c) the shared Budget is exhausted (hard max_steps/wallclock) or a tool call is
// detected as a dedup loop. On (c) it emits the explicit budget-exhausted Event
// (SC#2 shape). Exported per D-02; construct via NewLoop.
type LoopAgent struct {
	name          string
	maxIterations uint
	subs          []agent.Agent
}

// NewLoop returns an agent.Agent (the interface, D-02 factory). maxIter==0 means
// "iterate until escalate or budget exhaustion" (no iteration ceiling). The
// typed-nil guard (D-02) is implicit: a non-nil *LoopAgent is always boxed.
func NewLoop(name string, maxIter uint, subs ...agent.Agent) agent.Agent {
	return &LoopAgent{name: name, maxIterations: maxIter, subs: subs}
}

// Name is the Event Author / FindAgent key for this orchestrator.
func (a *LoopAgent) Name() string { return a.name }

// Description is the human/LLM-facing one-liner.
func (*LoopAgent) Description() string {
	return "re-runs sub-agents until maxIterations, a sub escalate, or budget exhaustion"
}

// Run drives the iteration loop. The budget is consumed ONCE PER TOOL CALL (D-11),
// and each consumed tool call is surfaced as exactly one step Event (WR-05), so
// steps_consumed always equals the number of yielded step Events — even for a turn
// carrying multiple tool calls (a multi-tool Event yields one step Event per call;
// a single-tool turn yields the original Event unchanged, SC#2: 25 step Events +
// 1 terminal = 26). A non-tool Event consumes no step and is yielded as-is. dedup
// is checked per tool call via the shared ring (D-09). Every yield is guarded
// (D-22). On a hard budget stop or a dedup loop it yields one explicit terminal
// Event then returns (the terminal replaces only the REMAINING tool calls, never
// an already-consumed one); on a sub escalate it yields the escalate Event then
// returns (the escalate Event ALWAYS precedes the iterator returning, D-21).
func (a *LoopAgent) Run(ic agent.InvocationContext) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		var stepsConsumed int
		for iterIdx := uint(0); a.maxIterations == 0 || iterIdx < a.maxIterations; iterIdx++ {
			for _, sub := range a.subs {
				subIC := ic.WithSubAgent(sub)
				subIC.Branch = joinBranch(joinBranch(ic.Branch, iterLabel(iterIdx)), sub.Name())

				for ev, err := range sub.Run(subIC) {
					if err != nil {
						yield(ev, err) // a REAL failure surfaces through the error slot, then stop
						return
					}

					calls := toolCalls(ev)
					if len(calls) == 0 {
						// A non-tool Event consumes no budget step; yield it as-is.
						if !yield(ev, nil) {
							return
						}
						if ev != nil && ev.Actions.Escalate {
							return // sub-driven termination (Req#5b), escalate already yielded (D-21)
						}
						continue
					}

					// Each tool call is ONE budgeted step (D-11) and ONE dedup observation
					// (D-18), and is surfaced as ONE step Event (WR-05): spend and emitted
					// step Events stay 1:1, so steps_consumed always equals the number of
					// yielded step Events. A budget/dedup trip mid-Event therefore never
					// discards an already-consumed tool call's step — those were already
					// yielded; the terminal Event only replaces the REMAINING calls. For a
					// single-tool turn the per-call Event equals the original (SC#2: 25 step
					// Events + 1 terminal = 26).
					preview := resultPreview(ev)
					for _, tc := range calls {
						stop, broke := a.guardToolCall(ic, ev, tc, preview, &stepsConsumed, yield)
						if broke {
							return // consumer broke, or a terminal Event was yielded
						}
						if stop {
							return
						}
					}
					if ev != nil && ev.Actions.Escalate {
						return // sub-driven termination (Req#5b), escalate already yielded (D-21)
					}
				}
			}
		}
	}
}

// guardToolCall applies the per-tool-call budget + dedup gates for ONE tool call
// of the triggering Event ev, and on success yields that tool call as one step
// Event (WR-05: spend and emitted step Events are 1:1). It returns:
//   - terminated=true when the loop must stop because a dedup/budget terminal Event
//     was already yielded (the terminal replaces only the REMAINING calls; any
//     already-consumed call was already surfaced as its own step Event).
//   - broke=true when the consumer returned false on the step-Event yield.
//
// *stepsConsumed is incremented on each successful step, so it always equals the
// count of step Events yielded so far.
func (a *LoopAgent) guardToolCall(
	ic agent.InvocationContext,
	ev *agent.Event,
	tc llm.ToolCall,
	preview []byte,
	stepsConsumed *int,
	yield func(*agent.Event, error) bool,
) (terminated, broke bool) {
	// B2 caller-canonicalizes contract: the LoopAgent produces the canonical args
	// itself, then hands opaque bytes to the Budget dedup ring (D-18).
	argsCanon := canonArgs(tc.Function.Arguments)

	// Dedup pre-check BEFORE the side effect re-runs (D-18).
	if dedup, reason := ic.Budget.BeforeToolCall(tc.Function.Name, argsCanon); dedup {
		_ = yield(a.terminalEvent(ic, reason, *stepsConsumed), nil)
		return true, false
	}

	// Budget consume (D-11). Only HARD terminal reasons (max_steps/wallclock) emit
	// budget-exhausted; the soft cap is a non-terminal fairness signal (D-12).
	ok, reason := ic.Budget.ConsumeStep()
	if !ok {
		_ = yield(a.terminalEvent(ic, reason, *stepsConsumed), nil)
		return true, false
	}
	*stepsConsumed++

	// Record the result preview as a progress veto (D-18): a changing preview
	// suppresses the next dedup (the loop is making progress), a stable preview lets
	// period-1/period-2 repeats terminate. The preview is the emitting Event's
	// assistant content; a real LlmAgent (Phase 3) passes the tool's bounded result.
	ic.Budget.AfterToolResult(tc.Function.Name, argsCanon, preview)

	// Surface this consumed tool call as exactly one step Event (WR-05). For a
	// single-tool Event the scoped copy equals the original.
	if !yield(scopeToToolCall(ev, tc), nil) {
		return false, true
	}
	return false, false
}

// scopeToToolCall returns a step Event representing a SINGLE tool call of ev. When
// ev carries exactly one tool call the original pointer is returned unchanged (no
// copy, byte-identical output); otherwise a shallow copy is made with its
// LLMResponse narrowed to just tc, so a multi-tool turn yields one step Event per
// budgeted tool call (WR-05). Actions (escalate/state) ride on the original Event
// shape and are preserved on every per-call copy.
func scopeToToolCall(ev *agent.Event, tc llm.ToolCall) *agent.Event {
	if ev == nil || ev.LLMResponse == nil || len(ev.LLMResponse.ToolCalls) <= 1 {
		return ev
	}
	scoped := *ev
	resp := *ev.LLMResponse
	resp.ToolCalls = []llm.ToolCall{tc}
	scoped.LLMResponse = &resp
	return &scoped
}

// terminalEvent builds the explicit budget-exhausted / dedup termination Event
// (D-04, SC#2 shape): Author is the loop name, Escalate=true, and StateDelta
// carries termination_reason/limit_hit/steps_consumed. termination is Event-only,
// never the iter error slot.
func (a *LoopAgent) terminalEvent(ic agent.InvocationContext, reason string, stepsConsumed int) *agent.Event {
	return &agent.Event{
		RequestID: ic.RequestID,
		SpanID:    ic.SpanID,
		Author:    a.name,
		Branch:    ic.Branch,
		Actions: agent.Actions{
			Escalate: true,
			StateDelta: map[string]any{
				"termination_reason": "budget_exhausted",
				"limit_hit":          reason,
				"steps_consumed":     stepsConsumed,
			},
		},
	}
}

// SubAgents returns the direct children in declaration order.
func (a *LoopAgent) SubAgents() []agent.Agent { return a.subs }

// FindAgent returns self when name matches, else recurses into the subs (D-01).
func (a *LoopAgent) FindAgent(name string) agent.Agent {
	return findInTree(a, a.subs, name)
}

// iterLabel renders the per-iteration Branch segment .iter-<N> (D-15).
func iterLabel(i uint) string {
	return "iter-" + strconv.FormatUint(uint64(i), 10)
}

// toolCalls returns the tool calls an Event carries, or nil for a non-tool Event.
func toolCalls(ev *agent.Event) []llm.ToolCall {
	if ev == nil || ev.LLMResponse == nil {
		return nil
	}
	return ev.LLMResponse.ToolCalls
}

// resultPreview is the bounded progress-veto payload for an Event's tool calls
// (D-18): the assistant content accompanying the call. A changing preview across
// repeated identical args signals progress and vetoes dedup; a stable preview lets
// a real loop terminate. Empty when the Event carries no LLM content.
func resultPreview(ev *agent.Event) []byte {
	if ev == nil || ev.LLMResponse == nil {
		return nil
	}
	return []byte(ev.LLMResponse.Content)
}

// canonArgs canonicalizes a tool call's JSON-string arguments into stable bytes
// for the dedup fingerprint (B2). When the arguments are valid JSON they are
// canonicalized through canonicaljson (sorted keys, literal numbers); otherwise
// the raw bytes are used verbatim so a malformed-args tool still fingerprints
// stably rather than erroring out of the loop.
func canonArgs(arguments string) []byte {
	var v any
	if err := json.Unmarshal([]byte(arguments), &v); err != nil {
		return []byte(arguments)
	}
	out, err := canonicaljson.Marshal(v)
	if err != nil {
		return []byte(arguments)
	}
	return out
}

