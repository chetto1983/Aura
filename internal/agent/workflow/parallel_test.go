package workflow_test

import (
	"context"
	"iter"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/workflow"
	"go.uber.org/goleak"
)

// Compile-time assertion that the exported ParallelAgent satisfies the cornerstone
// agent.Agent interface (D-02). If a method drifts the build breaks here.
var _ agent.Agent = (*workflow.ParallelAgent)(nil)

// slowAgent emits N events, sleeping `delay` before each, until cancelled or the
// consumer breaks. It drives the backpressure + break-early + escalate-cancel
// tests: a child that is mid-flight when the iterator stops draining MUST drain
// via runSub's done/ctx.Done() arms, never leaking its goroutine (D-23).
type slowAgent struct {
	name  string
	n     int
	delay time.Duration
}

func (a *slowAgent) Name() string           { return a.name }
func (*slowAgent) Description() string      { return "test: emits slow events until cancelled" }
func (*slowAgent) SubAgents() []agent.Agent { return nil }
func (a *slowAgent) FindAgent(name string) agent.Agent {
	if a.name == name {
		return a
	}
	return nil
}
func (a *slowAgent) Run(ic agent.InvocationContext) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		for i := 0; i < a.n; i++ {
			select {
			case <-ic.Ctx.Done():
				return // intentional cancel — exit cleanly, runSub turns this into (nil,nil)
			case <-time.After(a.delay):
			}
			if !yield(&agent.Event{Author: a.name, Branch: ic.Branch}, nil) {
				return // D-22 footgun 2: consumer broke, stop without a bare yield
			}
		}
	}
}

// boundedCounter consumes the SHARED budget counter up to `max` times (or until
// the budget refuses a step), recording each success on a shared *atomic.Int32 the
// test owns. Unlike CountingAgent it stops after `max` so the shared-budget test
// can script "3 children consume 1 each, then 3 more compete for the remaining 2".
type boundedCounter struct {
	name    string
	max     int
	counter *atomic.Int32 // external tally of successful ConsumeStep calls across the tree
}

func (a *boundedCounter) Name() string           { return a.name }
func (*boundedCounter) Description() string      { return "test: consumes shared budget up to max times" }
func (*boundedCounter) SubAgents() []agent.Agent { return nil }
func (a *boundedCounter) FindAgent(name string) agent.Agent {
	if a.name == name {
		return a
	}
	return nil
}
func (a *boundedCounter) Run(ic agent.InvocationContext) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		for i := 0; i < a.max; i++ {
			ok, _ := ic.Budget.ConsumeStep()
			if !ok {
				return // shared budget refused — stop
			}
			a.counter.Add(1)
			if !yield(&agent.Event{Author: a.name, Branch: ic.Branch}, nil) {
				return
			}
		}
	}
}

// sharedCounterTool wraps a sub-agent the way Phase 9 will expose one AS a tool. It
// MUST thread the SAME ic.Budget (the shared *atomic.Int32), never NewBudgetFromEnv
// — a fresh budget would silently reintroduce the depth³ trap (Pitfall 3). It runs
// the wrapped agent under the budget it was handed and counts successful steps.
type sharedCounterTool struct {
	name    string
	inner   *agenttest.CountingAgent
	counter *atomic.Int32
}

func (a *sharedCounterTool) Name() string { return a.name }
func (*sharedCounterTool) Description() string {
	return "test: sub-agent exposed as a tool, threads the shared budget"
}
func (a *sharedCounterTool) SubAgents() []agent.Agent { return []agent.Agent{a.inner} }
func (a *sharedCounterTool) FindAgent(name string) agent.Agent {
	if a.name == name {
		return a
	}
	return a.inner.FindAgent(name)
}
func (a *sharedCounterTool) Run(ic agent.InvocationContext) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		// Threads ic.Budget straight through — the shared counter, NOT a fresh one.
		before := a.inner.Calls
		for ev, err := range a.inner.Run(ic.WithSubAgent(a.inner)) {
			if !yield(ev, err) {
				return
			}
		}
		a.counter.Add(int32(a.inner.Calls - before))
	}
}

// budgetIC builds an InvocationContext whose Budget starts at maxSteps shared
// across the whole tree (SetMaxSteps resets the shared *atomic.Int32 at boot).
func budgetIC(t *testing.T, branch string, maxSteps int32) agent.InvocationContext {
	t.Helper()
	b, err := agent.NewBudgetFromEnv()
	if err != nil {
		t.Fatalf("NewBudgetFromEnv: %v", err)
	}
	b.SetMaxSteps(maxSteps)
	return agent.InvocationContext{Ctx: context.Background(), Branch: branch, Budget: b}
}

// TestParallelAgent_DepthChainBudgetShared_NotFresh is SC#3: Root is a
// ParallelAgent of 3 ParallelAgents, each of 3 CountingAgents (9 leaves). All nine
// share ONE *atomic.Int32 seeded at 25, so total successful ConsumeStep across the
// tree is ≤25 — NOT 25³ (15625). A fresh-per-child budget would blow past 25.
func TestParallelAgent_DepthChainBudgetShared_NotFresh(t *testing.T) {
	defer goleak.VerifyNone(t)

	const maxSteps = 25
	var leaves []*agenttest.CountingAgent
	var mids []agent.Agent
	for i := 0; i < 3; i++ {
		var trio []agent.Agent
		for j := 0; j < 3; j++ {
			c := &agenttest.CountingAgent{AgentName: "leaf"}
			leaves = append(leaves, c)
			trio = append(trio, c)
		}
		mids = append(mids, workflow.NewParallel("mid", trio...))
	}
	root := workflow.NewParallel("root", mids...)

	ic := budgetIC(t, "root", maxSteps)
	if _, err := drain(root.Run(ic)); err != nil {
		t.Fatalf("error slot must stay clean (D-04): %v", err)
	}

	total := 0
	for _, leaf := range leaves {
		total += leaf.Calls
	}
	if total > maxSteps {
		t.Fatalf("SC#3: total ConsumeStep successes across 9 leaves = %d, want ≤ %d (NOT %d³)", total, maxSteps, maxSteps)
	}
	if total == 0 {
		t.Fatalf("SC#3: tree consumed 0 steps — the shared counter was never reached")
	}
	if rem := ic.Budget.Remaining(); total+rem != maxSteps {
		t.Errorf("SC#3 accounting: consumed %d + remaining %d != seed %d", total, rem, maxSteps)
	}
}

// TestParallelAgent_SubAgentExposedAsTool_SharesCounter guards Pitfall 3: a
// sub-agent wrapped as a tool threads the SAME shared *atomic.Int32, so a 3-wide
// fan of tool-wrapped CountingAgents still totals ≤25, not 25×.
func TestParallelAgent_SubAgentExposedAsTool_SharesCounter(t *testing.T) {
	defer goleak.VerifyNone(t)

	const maxSteps = 25
	var counter atomic.Int32
	var tools []agent.Agent
	for i := 0; i < 3; i++ {
		tools = append(tools, &sharedCounterTool{
			name:    "tool",
			inner:   &agenttest.CountingAgent{AgentName: "wrapped"},
			counter: &counter,
		})
	}
	root := workflow.NewParallel("root", tools...)

	ic := budgetIC(t, "root", maxSteps)
	if _, err := drain(root.Run(ic)); err != nil {
		t.Fatalf("error slot must stay clean (D-04): %v", err)
	}
	if got := int(counter.Load()); got > maxSteps {
		t.Fatalf("tool-wrapped subs must share the counter: total = %d, want ≤ %d", got, maxSteps)
	}
	if counter.Load() == 0 {
		t.Fatalf("tool-wrapped subs consumed 0 — the shared budget was not threaded")
	}
}

// TestParallelAgent_EscalateFromAnyCancelsSiblings checks D-03/D-05: child[1]
// escalates, which fires the captured cancel; child[0]/child[2] (slow) are
// cancelled and drain (nil,nil). The escalate Event is observed and no goroutine
// leaks (goleak.VerifyNone).
func TestParallelAgent_EscalateFromAnyCancelsSiblings(t *testing.T) {
	defer goleak.VerifyNone(t)

	c0 := &slowAgent{name: "c0", n: 100, delay: 20 * time.Millisecond}
	c1 := &agenttest.EmitNThenEscalate{AgentName: "c1", N: 0} // escalates immediately
	c2 := &slowAgent{name: "c2", n: 100, delay: 20 * time.Millisecond}
	root := workflow.NewParallel("root", c0, c1, c2)

	ic := budgetIC(t, "root", 1000)
	evs, err := drain(root.Run(ic))
	if err != nil {
		t.Fatalf("intentional cancel must NOT surface as an error (D-05): %v", err)
	}

	var sawEscalate bool
	for _, ev := range evs {
		if ev != nil && ev.Actions.Escalate {
			sawEscalate = true
		}
	}
	if !sawEscalate {
		t.Fatalf("the escalate Event from c1 must be yielded (D-03 cancels siblings after)")
	}
	// The slow siblings cannot have run to completion (100 events each at 20ms);
	// the cancel must have cut them short well under 200 total events.
	if len(evs) > 50 {
		t.Errorf("escalate should cancel siblings quickly; got %d events (siblings not cancelled?)", len(evs))
	}
}

// TestParallelAgent_NoGoroutineLeak_When_ConsumerBreaksEarly is the D-23 / SC#1
// headline test: 3 slow children, the consumer breaks after the FIRST event. Every
// in-flight child must drain via done/ctx.Done() — goleak.VerifyNone proves zero
// goroutine leak.
func TestParallelAgent_NoGoroutineLeak_When_ConsumerBreaksEarly(t *testing.T) {
	defer goleak.VerifyNone(t)

	c0 := &slowAgent{name: "c0", n: 100, delay: 10 * time.Millisecond}
	c1 := &slowAgent{name: "c1", n: 100, delay: 10 * time.Millisecond}
	c2 := &slowAgent{name: "c2", n: 100, delay: 10 * time.Millisecond}
	root := workflow.NewParallel("root", c0, c1, c2)

	ic := budgetIC(t, "root", 1000)
	count := 0
	for ev, err := range root.Run(ic) {
		if err != nil {
			t.Fatalf("no error expected: %v", err)
		}
		_ = ev
		count++
		break // consumer leaves after one event while children are still producing
	}
	if count != 1 {
		t.Fatalf("consumer should have seen exactly 1 event before break, got %d", count)
	}
	// goleak.VerifyNone (deferred) fails if any child goroutine is still parked.
}

// TestParallelAgent_ChildrenShareParentBudget proves the shared atomic bounds the
// fan: with 5 steps and 3 children each willing to consume up to 3, the total
// successful ConsumeStep across the tree is exactly the seed (5) — the shared
// counter, not 3×3.
func TestParallelAgent_ChildrenShareParentBudget(t *testing.T) {
	defer goleak.VerifyNone(t)

	const maxSteps = 5
	var counter atomic.Int32
	c0 := &boundedCounter{name: "c0", max: 3, counter: &counter}
	c1 := &boundedCounter{name: "c1", max: 3, counter: &counter}
	c2 := &boundedCounter{name: "c2", max: 3, counter: &counter}
	root := workflow.NewParallel("root", c0, c1, c2)

	ic := budgetIC(t, "root", maxSteps)
	if _, err := drain(root.Run(ic)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := int(counter.Load()); got != maxSteps {
		t.Fatalf("children share one budget: total successes = %d, want exactly %d", got, maxSteps)
	}
	if rem := ic.Budget.Remaining(); rem != 0 {
		t.Errorf("budget should be fully drained: remaining = %d, want 0", rem)
	}
}

// TestParallelAgent_BackpressureAckChannel proves the ack channel makes the
// producer wait for the consumer: a slow consumer means a child cannot race ahead.
// We assert the child has produced at most one un-acked event while the consumer
// is still processing the first — the synchronous ack bounds in-flight work.
func TestParallelAgent_BackpressureAckChannel(t *testing.T) {
	defer goleak.VerifyNone(t)

	// A child that records, via a shared counter, how many events it has *handed
	// off* (sent to results). With synchronous ack the producer cannot get more
	// than one event ahead of a consumer that is still processing.
	var handedOff atomic.Int32
	child := &ackProbeAgent{name: "probe", n: 20, handedOff: &handedOff}
	root := workflow.NewParallel("root", child)

	ic := budgetIC(t, "root", 1000)
	maxAhead := int32(0)
	consumed := 0
	for ev, err := range root.Run(ic) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		_ = ev
		// Slow consumer: while we hold this event un-acked, the producer is blocked
		// waiting for our ack, so handedOff cannot exceed consumed+1.
		time.Sleep(5 * time.Millisecond)
		consumed++
		if ahead := handedOff.Load() - int32(consumed); ahead > maxAhead {
			maxAhead = ahead
		}
		if consumed >= 5 {
			break
		}
	}
	if maxAhead > 1 {
		t.Fatalf("backpressure violated: producer got %d events ahead of the consumer (want ≤ 1)", maxAhead)
	}
}

// ackProbeAgent emits n events, incrementing handedOff just before each yield so
// the test can measure how far ahead of the consumer the producer runs. The
// runSub ack gate is what keeps this to ≤1.
type ackProbeAgent struct {
	name      string
	n         int
	handedOff *atomic.Int32
}

func (a *ackProbeAgent) Name() string           { return a.name }
func (*ackProbeAgent) Description() string      { return "test: counts events handed to the fan-in channel" }
func (*ackProbeAgent) SubAgents() []agent.Agent { return nil }
func (a *ackProbeAgent) FindAgent(name string) agent.Agent {
	if a.name == name {
		return a
	}
	return nil
}
func (a *ackProbeAgent) Run(ic agent.InvocationContext) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		for i := 0; i < a.n; i++ {
			a.handedOff.Add(1)
			if !yield(&agent.Event{Author: a.name, Branch: ic.Branch}, nil) {
				return
			}
		}
	}
}

// TestParallelAgent_TOCTOUSafe_UnderConcurrentFan is a race-detector stress: many
// children compete for a counter of 1, exactly one wins. Run repeatedly (-count)
// under -race to shake out an over-spend (D-11) or a leaked goroutine.
func TestParallelAgent_TOCTOUSafe_UnderConcurrentFan(t *testing.T) {
	defer goleak.VerifyNone(t)

	const fan = 16
	var counter atomic.Int32
	var subs []agent.Agent
	for i := 0; i < fan; i++ {
		subs = append(subs, &boundedCounter{name: "c", max: 1, counter: &counter})
	}
	root := workflow.NewParallel("root", subs...)

	ic := budgetIC(t, "root", 1)
	if _, err := drain(root.Run(ic)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := counter.Load(); got != 1 {
		t.Fatalf("TOCTOU: exactly one of %d racing children may consume the single step, got %d", fan, got)
	}
}

// erroringAgent emits one real failure through the error slot — a genuine
// LLM/tool error, the ONLY thing D-04 lets through the error slot.
type erroringAgent struct {
	name string
	err  error
}

func (a *erroringAgent) Name() string           { return a.name }
func (*erroringAgent) Description() string      { return "test: yields a real error" }
func (*erroringAgent) SubAgents() []agent.Agent { return nil }
func (a *erroringAgent) FindAgent(name string) agent.Agent {
	if a.name == name {
		return a
	}
	return nil
}
func (a *erroringAgent) Run(_ agent.InvocationContext) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		yield(nil, a.err)
	}
}

// TestParallelAgent_RealChildError_SurfacesThroughErrorSlot proves D-04: a REAL
// child failure (not an intentional cancel) reaches the consumer through the iter
// error slot, while the run still drains every goroutine cleanly.
func TestParallelAgent_RealChildError_SurfacesThroughErrorSlot(t *testing.T) {
	defer goleak.VerifyNone(t)

	wantErr := context.DeadlineExceeded // any non-nil sentinel; stands in for an LLM/tool error
	bad := &erroringAgent{name: "bad", err: wantErr}
	ok := &slowAgent{name: "ok", n: 100, delay: 20 * time.Millisecond}
	root := workflow.NewParallel("root", bad, ok)

	ic := budgetIC(t, "root", 1000)
	_, err := drain(root.Run(ic))
	if err == nil {
		t.Fatalf("a real child error must surface through the error slot (D-04)")
	}
}

// TestParallelAgent_TreeIntrospection covers the orchestrator's tree contract:
// Description is non-empty, SubAgents returns the direct children in order, and
// FindAgent locates a deeply-nested leaf (D-01 recursion) while returning nil for
// an absent name.
func TestParallelAgent_TreeIntrospection(t *testing.T) {
	leaf := &agenttest.CountingAgent{AgentName: "deep-leaf"}
	mid := workflow.NewParallel("mid", leaf)
	root := workflow.NewParallel("root", mid)

	if root.Description() == "" {
		t.Errorf("Description must be a non-empty one-liner")
	}
	subs := root.SubAgents()
	if len(subs) != 1 || subs[0].Name() != "mid" {
		t.Fatalf("SubAgents: want [mid], got %v", subs)
	}
	if got := root.FindAgent("deep-leaf"); got == nil || got.Name() != "deep-leaf" {
		t.Errorf("FindAgent must recurse to the nested leaf (D-01), got %v", got)
	}
	if got := root.FindAgent("absent"); got != nil {
		t.Errorf("FindAgent must return nil for an absent name, got %v", got)
	}
}
