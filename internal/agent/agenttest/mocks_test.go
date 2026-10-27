package agenttest_test

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
)

// testIC builds an InvocationContext with a Budget seeded to maxSteps via
// SetMaxSteps so drain tests are hermetic (no AURA_LOOP_* env dependence).
func testIC(t *testing.T, maxSteps int32) agent.InvocationContext {
	t.Helper()
	b, err := agent.NewBudget(agent.BudgetOptions{})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	b.SetMaxSteps(maxSteps)
	return agent.InvocationContext{Ctx: context.Background(), Budget: b, Branch: "root"}
}

// TestMocks_RunYieldDiscipline drains each mock's Run under a small test Budget
// and asserts no panic + correct termination per mock (W5: the iter.Seq2
// yield-after-false guard, D-22, is a RUNTIME footgun — build/vet cannot catch
// it, so it must be exercised here before the workflow tests depend on these fixtures).
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
