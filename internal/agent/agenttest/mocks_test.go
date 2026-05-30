package agenttest_test

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
)

// testIC builds an InvocationContext with a Budget seeded to maxSteps via
// SetMaxSteps so drain tests are hermetic (no AURA_LOOP_* env dependence). The
// shared *atomic.Int32 lives inside the returned Budget; CountingAgent consumes
// it directly (never a fresh NewBudgetFromEnv — Pitfall 3).
func testIC(t *testing.T, maxSteps int32) agent.InvocationContext {
	t.Helper()
	b, err := agent.NewBudgetFromEnv()
	if err != nil {
		t.Fatalf("NewBudgetFromEnv: %v", err)
	}
	b.SetMaxSteps(maxSteps)
	return agent.InvocationContext{Ctx: context.Background(), Budget: b, Branch: "root"}
}

// TestMocks_RunYieldDiscipline drains each mock's Run under a small test Budget
// and asserts no panic + correct termination per mock (W5: the iter.Seq2
// yield-after-false guard, D-22, is a RUNTIME footgun — build/vet cannot catch
// it, so it must be exercised here before Plan 05/06 depend on these fixtures).
func TestMocks_RunYieldDiscipline(t *testing.T) {
	const drainCap = 3

	tests := []struct {
		name      string
		mock      agent.Agent
		wantMin   int // minimum events expected within the drain cap
		wantMax   int // maximum events expected within the drain cap
		assertEnd func(t *testing.T, got []*agent.Event)
	}{
		{
			name:    "InfiniteToolCallAgent bounded by consumer break",
			mock:    &agenttest.InfiniteToolCallAgent{ToolName: "noop", ToolArgs: `{"k":1}`},
			wantMin: drainCap,
			wantMax: drainCap, // never self-terminates; the break at drainCap is the only stop
			assertEnd: func(t *testing.T, got []*agent.Event) {
				for i, ev := range got {
					if ev.LLMResponse == nil || len(ev.LLMResponse.ToolCalls) != 1 {
						t.Fatalf("event %d: want exactly one tool call, got %+v", i, ev.LLMResponse)
					}
					if name := ev.LLMResponse.ToolCalls[0].Function.Name; name != "noop" {
						t.Errorf("event %d: tool call name = %q, want the SAME %q every step", i, name, "noop")
					}
				}
			},
		},
		{
			name:    "EmitNThenEscalate terminates after escalate",
			mock:    &agenttest.EmitNThenEscalate{N: 2},
			wantMin: 3, // 2 normal + 1 escalate, all within the 3-step cap
			wantMax: 3,
			assertEnd: func(t *testing.T, got []*agent.Event) {
				last := got[len(got)-1]
				if !last.Actions.Escalate {
					t.Errorf("final event Escalate = false, want true (escalate is the terminal signal)")
				}
				for i := 0; i < len(got)-1; i++ {
					if got[i].Actions.Escalate {
						t.Errorf("event %d escalated early; only the final event may escalate", i)
					}
				}
			},
		},
		{
			name:    "RecordingAgent records branch and emits canned events",
			mock:    &agenttest.RecordingAgent{Events: []*agent.Event{{}, {}}},
			wantMin: 2,
			wantMax: 2,
			assertEnd: func(t *testing.T, got []*agent.Event) {
				for i, ev := range got {
					if ev.Branch != "root" {
						t.Errorf("event %d Branch = %q, want %q (stamped from the InvocationContext)", i, ev.Branch, "root")
					}
					if ev.Author == "" {
						t.Errorf("event %d Author empty; SetAuthorIfEmpty should have stamped the recorder name", i)
					}
				}
			},
		},
		{
			name:    "CountingAgent consumes shared budget then terminates",
			mock:    &agenttest.CountingAgent{},
			wantMin: drainCap, // consumes the 3 seeded steps, then yields a terminal event
			wantMax: drainCap, // the break at drainCap fires before the terminal event under a >=3 budget
			assertEnd: func(t *testing.T, got []*agent.Event) {
				// drained 3 successful steps under a 3-step budget; the consumer
				// break at drainCap stops before the budget-refusal event.
				_ = got
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ic := testIC(t, drainCap)
			got := drain(t, tt.mock, ic, drainCap)
			if len(got) < tt.wantMin || len(got) > tt.wantMax {
				t.Fatalf("drained %d events, want within [%d,%d]", len(got), tt.wantMin, tt.wantMax)
			}
			tt.assertEnd(t, got)
		})
	}
}

// TestMocks_CountingAgent_SharedCounter proves CountingAgent consumes the
// INJECTED shared budget — when drained without an artificial cap it stops at
// exactly the seeded step count (not a fresh NewBudgetFromEnv default of 25),
// which is the SC#3 shared-counter guarantee in miniature.
func TestMocks_CountingAgent_SharedCounter(t *testing.T) {
	const seeded = 4
	ic := testIC(t, seeded)
	m := &agenttest.CountingAgent{}

	events := drain(t, m, ic, 1_000) // generous cap; the budget, not the cap, must stop it
	if m.Calls != seeded {
		t.Fatalf("CountingAgent.Calls = %d, want %d (must consume the injected shared budget, not a fresh one)", m.Calls, seeded)
	}
	if ic.Budget.Remaining() != 0 {
		t.Errorf("shared budget Remaining = %d, want 0 (mock drained the same *atomic.Int32)", ic.Budget.Remaining())
	}
	last := events[len(events)-1]
	if last.Actions.StateDelta["limit_hit"] != "max_steps" {
		t.Errorf("terminal event limit_hit = %v, want %q", last.Actions.StateDelta["limit_hit"], "max_steps")
	}
}

// TestMocks_InfiniteToolCall_BreakAfterOne_NoPanic exercises D-22 footgun 2: a
// consumer that breaks the range after the FIRST event must not trip a
// "continued iteration after function for loop body returned false" panic. The
// yield-after-false guard (if !yield(...) { return }) is the only thing that
// makes this safe, and it has no compile/vet check — only this runtime drain.
func TestMocks_InfiniteToolCall_BreakAfterOne_NoPanic(t *testing.T) {
	ic := testIC(t, 3)
	m := &agenttest.InfiniteToolCallAgent{}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("break-after-one panicked (D-22 yield guard missing): %v", r)
		}
	}()

	count := 0
	for ev, err := range m.Run(ic) {
		if err != nil {
			t.Fatalf("unexpected error from InfiniteToolCallAgent: %v", err)
		}
		if ev == nil {
			t.Fatal("nil event from InfiniteToolCallAgent")
		}
		count++
		break // the consumer break is the exit; the guard must absorb it cleanly
	}
	if count != 1 {
		t.Fatalf("consumed %d events, want exactly 1 before break", count)
	}
}

// drain runs mock.Run(ic) with the mandatory two-var range form (D-22 footgun 4:
// the one-var form silently drops the error) and collects up to cap events,
// breaking afterwards. A panic here fails the test loud via Go's test harness.
func drain(t *testing.T, mock agent.Agent, ic agent.InvocationContext, cap int) []*agent.Event {
	t.Helper()
	var got []*agent.Event
	for ev, err := range mock.Run(ic) {
		if err != nil {
			t.Fatalf("unexpected error draining %s: %v", mock.Name(), err)
		}
		got = append(got, ev)
		if len(got) >= cap {
			break
		}
	}
	return got
}
