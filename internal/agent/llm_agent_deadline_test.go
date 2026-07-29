package agent_test

// Fix-plan 1.1 (Wallclock finalize DOA) black-box Run-loop coverage. Production
// (internal/runner/runner.go) drives ic.Ctx from budget.WithDeadline(ctx): an
// ABSOLUTE deadline equal to the SAME wallclock cutoff ConsumeStep checks
// (internal/agent/budget.go). A wallclock trip therefore means ic.Ctx is ALSO
// already Done at that instant. The one-shot recovery turn (maybeRecover +
// skipBudgetGate, llm_agent.go) rides the SAME context.WithTimeout(ic.Ctx, ...)
// call as an ordinary turn — so without severing the expired deadline for that
// one recovery turn, it would be dead-on-arrival too. These tests drive the real
// Run loop with a ctx-capturing fake client so the assertion is on the ACTUAL ctx
// handed to the LLM client, not just on Run's return value (a fake client, unlike
// a real network client, never itself observes ctx cancellation).

import (
	"context"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// ctxCapturingClient mirrors agenttest.FakeClient's scripted-turn behavior but
// additionally records, per call, whether ctx was already Done and its deadline —
// the signal a real network client's dial/write would honor and a scripted fake
// otherwise hides.
type ctxCapturingClient struct {
	turns []agenttest.FakeTurn
	next  int

	ctxErrs   []error
	deadlines []time.Time
}

func (c *ctxCapturingClient) Stream(ctx context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	c.ctxErrs = append(c.ctxErrs, ctx.Err())
	d, _ := ctx.Deadline()
	c.deadlines = append(c.deadlines, d)

	var turn agenttest.FakeTurn
	if c.next < len(c.turns) {
		turn = c.turns[c.next]
	}
	c.next++
	if turn.Err != nil {
		return nil, turn.Err
	}
	ch := make(chan llm.Chunk, len(turn.Chunks)+1)
	for _, chunk := range turn.Chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

var _ llm.Client = (*ctxCapturingClient)(nil)

func newCtxCapturingAgent(t *testing.T, client *ctxCapturingClient) *agent.LlmAgent {
	t.Helper()
	return agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:     client,
		LLM:        llm.Config{Model: "test-model", Provider: "test-provider", TotalTimeoutSec: 30},
		Registry:   testRegistry(),
		PreviewCap: 2048,
		RunDir:     t.TempDir(),
		SessionID:  uuid.Must(uuid.NewV7()).String(),
		UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: "ciao"}},
	})
}

// TestRun_RecoveryTurnSeversExpiredWallclockDeadline is the fix-plan 1.1 RED test
// for the recovery turn specifically (llm_agent.go:251, ridden by skipBudgetGate —
// NOT llm_agent_finalize.go/llm_agent_completion.go, which are fixed independently).
// It reproduces the production shape: ic.Ctx already Done (mirroring
// budget.WithDeadline having fired) PLUS an injected Budget clock that trips
// "wallclock" on the very first ConsumeStep (same technique as TestFinalizeWallclock).
// The single scripted turn is therefore the ONE recovery call riding outside the
// budget gate; its ctx must NOT be inherited-Done.
func TestRun_RecoveryTurnSeversExpiredWallclockDeadline(t *testing.T) {
	recordingProvider(t)
	client := &ctxCapturingClient{turns: []agenttest.FakeTurn{agenttest.TextChunks("stop", finalizeAnswer)}}
	a := newCtxCapturingAgent(t, client)

	base := time.Now()
	calls := 0
	clock := func() time.Time {
		calls++
		if calls == 1 {
			return base // construction anchor
		}
		return base.Add(2 * time.Hour) // every ConsumeStep check reads past the deadline
	}
	b, err := agent.NewBudget(agent.BudgetOptions{MaxWallclockSec: new(1), Now: clock})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}

	// ic.Ctx already Done, independent of the injected Budget clock (context's own
	// timer does not read the fake clock) — mirroring the real wallclock-fired
	// moment in production where both are past the SAME deadline.
	expiredCtx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
	defer cancel()
	if expiredCtx.Err() == nil {
		t.Fatal("test setup: expiredCtx must already be Done")
	}

	ic := agent.InvocationContext{Ctx: expiredCtx, RequestID: uuid.Must(uuid.NewV7()), Budget: b}
	evs, rerr := collect(a.Run(ic))
	if rerr != nil {
		t.Fatalf("wallclock trip surfaced an error slot: %v", rerr)
	}
	last := evs[len(evs)-1]
	if last.LLMResponse == nil || last.LLMResponse.Content != finalizeAnswer {
		t.Errorf("terminal content = %+v, want the recovery answer %q", last.LLMResponse, finalizeAnswer)
	}
	if len(client.ctxErrs) != 1 {
		t.Fatalf("client called %d times, want exactly 1 (the recovery turn)", len(client.ctxErrs))
	}
	if client.ctxErrs[0] != nil {
		t.Fatalf("recovery turn ctx.Err() = %v, want nil (a fresh deadline, not inherited from the expired ic.Ctx — DOA)", client.ctxErrs[0])
	}
	if remaining := time.Until(client.deadlines[0]); remaining < 4*time.Second {
		t.Fatalf("recovery turn deadline remaining = %s, want about 30s (fresh TotalTimeoutSec)", remaining)
	}
}

// TestRun_NormalTurnHonorsUnexpiredCtxDeadline is the regression guard (hard
// constraint #1): an ORDINARY turn — no budget trip, no recovery — must still
// derive its call ctx from ic.Ctx, so a real (not-yet-expired) deadline still
// bounds it. ic.Ctx's deadline (soon) is deliberately much sooner than
// TotalTimeoutSec (30s); the client must observe the SOONER ic.Ctx-derived
// deadline, proving the ordinary-turn branch was not widened to a fresh
// TotalTimeoutSec-only window.
func TestRun_NormalTurnHonorsUnexpiredCtxDeadline(t *testing.T) {
	recordingProvider(t)
	client := &ctxCapturingClient{turns: []agenttest.FakeTurn{agenttest.TextChunks("stop", finalizeAnswer)}}
	a := newCtxCapturingAgent(t, client)

	boundedDeadline := time.Now().Add(2 * time.Second)
	boundedCtx, cancel := context.WithDeadline(context.Background(), boundedDeadline)
	defer cancel()

	ic := newIC(t, agent.BudgetOptions{MaxSteps: new(50)})
	ic.Ctx = boundedCtx

	evs, rerr := collect(a.Run(ic))
	if rerr != nil {
		t.Fatalf("Run errored: %v", rerr)
	}
	last := evs[len(evs)-1]
	if last.LLMResponse == nil || last.LLMResponse.Content != finalizeAnswer {
		t.Errorf("terminal content = %+v, want the ordinary content-stop answer %q", last.LLMResponse, finalizeAnswer)
	}
	if len(client.ctxErrs) != 1 {
		t.Fatalf("client called %d times, want exactly 1 (the single ordinary turn)", len(client.ctxErrs))
	}
	if got := client.deadlines[0]; got.After(boundedDeadline.Add(500 * time.Millisecond)) {
		t.Fatalf("ordinary turn deadline = %s, want ~= the ic.Ctx deadline %s (still bounded, not widened to a fresh 30s)", got, boundedDeadline)
	}
}
