// Pattern derivato da google/adk-go v1.4.0 agent/workflowagents/parallelagent/agent.go
// (Apache 2.0). Adattato per Aura con SC#2 budget exhaustion + SC#3
// child-inherits-remaining + SC#4 UUIDv7 OTel-compat.
//
// The channel choreography (errgroup fan-out + per-event ack backpressure + a
// fan-in results channel yielded SERIALLY from the iterator frame) is stolen
// near-verbatim from adk's parallelagent. Two behaviours DIVERGE from adk, both
// marked inline below:
//   - D-03: escalate from any child fires a captured context.CancelFunc to stop
//     siblings — NOT a fake sentinel error. A fake error would pollute
//     errgroup.Wait() and surface through the iter error slot, which D-04 reserves
//     for REAL LLM/tool failures only.
//   - D-05: a sibling cancelled by that escalate (or by a real first-error) drains
//     by returning nil, NOT ctx.Err() (adk yields ctx.Err() at agent.go:142).
//     Intentional cancellation is not error noise, so the error slot stays clean.
//
// Concurrency invariants (D-22/D-23): the iterator NEVER yields from a spawned
// goroutine — children fan-in to `results` and the iterator frame yields each
// pair serially (footgun 3). Every channel op in runSub is a multi-arm select
// with a `case <-done`/`case <-ctx.Done()` exit so a consumer break-early leaks
// no goroutine; the frame runs `defer cancel()` + `defer close(done)`; the spawn
// loop is guarded by `if egCtx.Err() != nil` (Go #61611).
//
// Budget (D-09/D-10): each child forks ic.Budget.Child(len(subs)) — a DISTINCT
// dedup ring (no cross-branch false positives) that still shares the one
// *atomic.Int32 step counter by pointer. That shared pointer is what makes SC#3
// true: a depth-3 fan-3 tree consumes ≤ max_steps total, NOT max_steps³.
package workflow

import (
	"context"
	"fmt"
	"iter"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/panicobs"
	"golang.org/x/sync/errgroup"
)

// ParallelAgent runs its sub-agents concurrently, fans their Events into a single
// channel, and yields each (Event, error) serially from the iterator frame. An
// Escalate Event from any child cancels the siblings (D-03); a real failure from
// any child surfaces once through the error slot (D-04). Exported per D-02;
// construct via NewParallel.
type ParallelAgent struct {
	name string
	subs []agent.Agent
}

// NewParallel returns an agent.Agent (the interface, D-02 factory) that runs subs
// concurrently. The typed-nil guard (D-02) is implicit: a non-nil *ParallelAgent
// is always boxed in the interface, never a typed nil.
func NewParallel(name string, subs ...agent.Agent) agent.Agent {
	return &ParallelAgent{name: name, subs: subs}
}

// Name is the Event Author / FindAgent key for this orchestrator.
func (a *ParallelAgent) Name() string { return a.name }

// Description is the human/LLM-facing one-liner.
func (*ParallelAgent) Description() string {
	return "runs sub-agents concurrently, fanning their events in; an escalate cancels siblings"
}

// result is the fan-in payload one child sends per Event: the Event/err plus a
// per-Event ack channel the iterator closes once it has yielded, giving the child
// synchronous backpressure (no unbounded buffer under a slow consumer).
type result struct {
	event *agent.Event
	err   error
	ack   chan struct{}
}

// Run fans the subs out under one errgroup, fans their Events into `results`, and
// yields each pair serially from this frame. It steals adk's channel structure
// with the two documented divergences (D-03 captured-cancel, D-05 drain-nil).
func (a *ParallelAgent) Run(ic agent.InvocationContext) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		eg, egCtx := errgroup.WithContext(ic.Ctx) // egCtx cancels on the first non-nil child error
		// D-03: our OWN cancel for escalate — never a fake error into errgroup.Wait().
		egCtx, cancel := context.WithCancel(egCtx)
		defer cancel() // D-23: always release the context

		results := make(chan result)
		done := make(chan bool)

		for _, sub := range a.subs {
			// W6: the child InvocationContext is built with the REAL two-step API —
			// there is no single ic-level child-with-context method on
			// InvocationContext. WithContext/WithSubAgent return copies (D-24);
			// Budget.Child forks a distinct dedup ring while sharing the
			// *atomic.Int32 (D-09). Branch is dot-joined per child (D-15).
			childIC := ic.WithContext(egCtx).WithSubAgent(sub)
			childIC.Branch = joinBranch(joinBranch(ic.Branch, a.name), sub.Name())
			childIC.Budget = ic.Budget.Child(len(a.subs))

			eg.Go(func() error {
				if egCtx.Err() != nil { // D-23: Go #61611 spawn-loop guard
					return nil
				}
				return runSub(egCtx, childIC, sub, results, done)
			})
		}

		// One goroutine closes results after every child returns. A REAL child
		// failure (runSub returned a non-nil err) is forwarded ONCE through results
		// so the consumer sees it in the error slot (D-04) — adk does the same; the
		// 2-arm select keeps this goroutine from leaking when the consumer broke.
		go func() {
			if err := eg.Wait(); err != nil {
				select {
				case results <- result{err: err}:
				case <-done:
				}
			}
			close(results)
		}()

		defer close(done) // D-23: signals every parked child to drain on early break
		for res := range results {
			cont := yield(res.event, res.err)
			if res.ack != nil {
				close(res.ack) // backpressure ack: the child may now produce its next Event
			}
			if res.event != nil && res.event.Actions.Escalate {
				cancel() // D-03: escalate → cancel siblings (captured cancel, not an error)
			}
			if !cont {
				return // consumer break: defer close(done) drains every child cleanly
			}
		}
	}
}

// runSub streams one child's Events into results with synchronous ack backpressure.
// Every channel op is a multi-arm select so the child always has an exit path: on
// consumer break (`<-done`) or intentional cancel (`<-ctx.Done()`) it returns nil
// — NOT ctx.Err() (D-05) — so the iter error slot carries only real failures.
func runSub(
	ctx context.Context,
	ic agent.InvocationContext,
	sub agent.Agent,
	results chan<- result,
	done <-chan bool,
) (err error) {
	defer func() {
		if r := recover(); r != nil {
			panicobs.Record(panicobs.SiteWorkflowParallel)
			err = fmt.Errorf("parallel child %q panic: %v", sub.Name(), r)
		}
	}()
	for ev, err := range sub.Run(ic) {
		if err != nil {
			return err // a REAL failure surfaces through the error slot (D-04)
		}
		ack := make(chan struct{})
		select {
		case <-done:
			return nil // consumer broke (D-05: clean drain, not ctx.Err())
		case <-ctx.Done():
			return nil // sibling cancelled by escalate/first-error (D-05: nil, not ctx.Err())
		case results <- result{event: ev, ack: ack}:
			select {
			case <-ack: // iterator yielded this Event; produce the next one
			case <-done:
				return nil
			case <-ctx.Done():
				return nil
			}
		}
	}
	return nil
}

// SubAgents returns the direct children in declaration order.
func (a *ParallelAgent) SubAgents() []agent.Agent { return a.subs }

// FindAgent returns self when name matches, else recurses into the subs (D-01).
func (a *ParallelAgent) FindAgent(name string) agent.Agent {
	return findInTree(a, a.subs, name)
}
