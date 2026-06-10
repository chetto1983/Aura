// Package agent owns the cornerstone runtime contract every later phase
// implements or consumes: the open Agent interface, the single-Run-scoped
// InvocationContext, the forward-compat Event/Actions/LLMResponse shape, the
// OTel-correct trace IDs, and the Budget tree that bounds a run.
//
// The Agent interface is OPEN by design (no unexported seal): Phase 3 LlmAgent
// and Phase 9 swarm implement it directly. This diverges from google/adk-go,
// which seals its interface via an unexported internal() method so only its own
// constructors can implement it — adk's own docs note they are moving toward an
// open interface, which is the premise Aura locks in now (D-01).
package agent

import (
	"context"
	"iter"

	"github.com/google/uuid"
)

// Agent is the cornerstone contract every later phase implements or consumes.
// Open by design (no unexported seal) — Phase 3 LlmAgent and Phase 9 swarm
// implement this directly. Pattern derivato da google/adk-go v1.4.0
// agent/agent.go (Apache 2.0); the internal() seal is intentionally removed (D-01).
type Agent interface {
	// Name is the stable identifier used as the Event Author for this agent's
	// runtime-emitted events and as the lookup key for FindAgent.
	Name() string
	// Description is the human/LLM-facing one-liner (Phase 3 surfaces it to the model).
	Description() string
	// Run executes one invocation and streams Events. The error slot of the
	// iter.Seq2 carries only REAL failures (LLM/tool errors); termination and
	// budget exhaustion are signalled by an explicit Event, never the error slot
	// (D-04). Agents that consume Budget internally should implement BudgetOwner so
	// workflow parents do not double-charge their emitted events.
	Run(InvocationContext) iter.Seq2[*Event, error]
	// SubAgents returns the direct children (nil for leaf agents).
	SubAgents() []Agent
	// FindAgent returns self if name matches, else recurses into SubAgents;
	// nil if not found (collapses adk's FindAgent/FindSubAgent split).
	FindAgent(name string) Agent
}

// BudgetOwner is an optional Agent capability: agents that already enforce their
// own Budget gates expose it so workflow parents can remain observational instead
// of double-charging the same shared budget.
type BudgetOwner interface {
	OwnsBudget() bool
}

// AgentOwnsBudget reports whether an agent has opted into owning its own budget
// gates. Workflow parents use this to keep the single-budget-owner contract for a
// composed agent tree.
func AgentOwnsBudget(a Agent) bool {
	owner, ok := a.(BudgetOwner)
	return ok && owner.OwnsBudget()
}

// InvocationContext is single-Run-scoped; never store on a long-lived struct,
// never cache, never share across invocations. It is passed by value down the
// agent tree; WithContext/WithSubAgent always return a COPY and never mutate the
// receiver (D-24). Ctx is a NAMED field, never embedded — embedding invites
// passing the InvocationContext where a context.Context is expected and storing
// it, which the single-Run-scope invariant forbids.
type InvocationContext struct {
	Ctx          context.Context // request-scoped cancellation/deadline (named, never embedded)
	Agent        Agent           // the agent currently executing (self-reference for workflow recursion)
	RequestID    uuid.UUID       // UUIDv7, 16 bytes — OTel/W3C TraceID + run_id shape (D-16), minted once at root
	SpanID       [8]byte         // 8-byte OTel/W3C SpanID shape (D-16), per-node; minting DEFERRED to the OTel slice (WR-04) — stays [8]byte{} in Phase 2
	ParentSpanID *[8]byte        // nil at root; the parent node's SpanID otherwise — chaining DEFERRED to the OTel slice (WR-04)
	Branch       string          // hierarchical LABEL only ("root.iter-2.worker-3"); hierarchy comes from span IDs (D-15)
	Budget       *Budget         // shared budget tree (same *atomic.Int32 across the whole tree, D-10)
}

// WithContext returns a copy of ic with a replaced Ctx. The receiver is never
// mutated (D-24).
func (ic InvocationContext) WithContext(ctx context.Context) InvocationContext {
	c := ic
	c.Ctx = ctx
	return c
}

// WithSubAgent returns a copy of ic re-pointed at sub, keeping the SAME *Budget
// (same *atomic.Int32 step counter AND the same dedup ring) so a single logical
// reasoning thread sees its own cross-iteration repeats (D-09). The receiver is
// never mutated (D-24). Parallel branches use Budget.Child instead to fork a
// distinct dedup ring while still sharing the atomic counter.
func (ic InvocationContext) WithSubAgent(sub Agent) InvocationContext {
	c := ic
	c.Agent = sub
	return c
}
