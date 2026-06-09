// Package workflow holds the deterministic, non-concurrent orchestrators
// (SequentialAgent, LoopAgent) plus the concurrent ParallelAgent (Plan 06). They
// compose the cornerstone agent.Agent interface: constructors return the
// interface (factory pattern, like net.Pipe / context.WithCancel) so trees nest
// cleanly, while the structs stay exported for the compile-time satisfaction
// asserts the SPEC wants (D-02).
//
// Pattern derivato da google/adk-go v1.4.0 agent/workflowagents/sequentialagent/agent.go
// (Apache 2.0). Adattato per Aura con SC#2 budget exhaustion + SC#3
// child-inherits-remaining + SC#4 UUIDv7 OTel-compat.
//
// Aura divergences from adk's sequential control flow:
//   - adk seals its Agent interface and reads SubAgents() off the InvocationContext
//     agent; Aura's interface is open (D-01) and SequentialAgent holds subs directly.
//   - early-return on a sub Escalate event (Req#4) — adk's sequentialAgent has no
//     escalate short-circuit; Aura adds it so a sub can stop the chain.
//   - each sub runs under ic.WithSubAgent(sub), sharing the SAME Budget + dedup ring
//     (one logical reasoning thread, D-09); Branch is dot-joined per child (D-15).
package workflow

import (
	"iter"

	"github.com/chetto1983/aura/internal/agent"
)

// SequentialAgent runs its sub-agents exactly once each, in declaration order,
// streaming every sub Event upward. It returns early the moment a sub emits an
// Event with Actions.Escalate=true: that sub's escalate Event is yielded, then no
// further sub is invoked (Req#4). Exported per D-02; construct via NewSequential.
type SequentialAgent struct {
	name string
	subs []agent.Agent
}

// NewSequential returns an agent.Agent (the interface, D-02 factory) that runs
// subs once each in order. The typed-nil guard (D-02) is implicit: this always
// returns a non-nil *SequentialAgent boxed in the interface, never a typed nil.
func NewSequential(name string, subs ...agent.Agent) agent.Agent {
	return &SequentialAgent{name: name, subs: subs}
}

// Name is the Event Author / FindAgent key for this orchestrator.
func (a *SequentialAgent) Name() string { return a.name }

// Description is the human/LLM-facing one-liner.
func (*SequentialAgent) Description() string {
	return "runs sub-agents once each in order, stopping early on a sub escalate"
}

// Run iterates the subs once each in order. Each sub runs under
// ic.WithSubAgent(sub) (shared budget + dedup ring, D-09) with the Branch
// dot-joined to <branch>.<childName> (D-15). Every yield is guarded
// (if !yield(...) { return }, D-22 footgun 2). When a sub emits an escalate Event
// the iterator yields it and returns without invoking later subs (Req#4).
func (a *SequentialAgent) Run(ic agent.InvocationContext) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		for _, sub := range a.subs {
			subIC := ic.WithSubAgent(sub)
			subIC.Branch = joinBranch(ic.Branch, sub.Name())
			for ev, err := range sub.Run(subIC) {
				if !yield(ev, err) {
					return
				}
				// A sub error aborts the chain (parity with LoopAgent): later subs must
				// not run in a degraded state after a known failure (Req#4).
				if err != nil {
					return
				}
				if ev != nil && ev.Actions.Escalate {
					return
				}
			}
		}
	}
}

// SubAgents returns the direct children in declaration order.
func (a *SequentialAgent) SubAgents() []agent.Agent { return a.subs }

// FindAgent returns self when name matches, else recurses into the subs (D-01
// FindAgent contract); nil when not found anywhere in the subtree.
func (a *SequentialAgent) FindAgent(name string) agent.Agent {
	return findInTree(a, a.subs, name)
}
