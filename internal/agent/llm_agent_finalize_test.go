package agent_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// finalizeAnswer is the synthesized prose the scripted finalize turn returns.
// Load-bearing: the assertions below check the terminal Event carries exactly it.
const finalizeAnswer = "Ecco la risposta finale sintetizzata dai risultati raccolti."

// stubLeadIn mirrors the D-09 deterministic-stub lead-in constant in
// llm_agent_finalize.go — the production literal is unexported, so the test pins it
// here too (a drift makes TestFinalizeFallback fail loudly).
const stubLeadIn = "Non sono riuscito a completare la sintesi finale, ma ecco i dati che ho raccolto:"

// echoTurn is one scripted echo(same-args) tool call — the dedup-tripping turn.
func echoTurn() agenttest.FakeTurn {
	return agenttest.ToolCallTurn(agenttest.MakeToolCall("c", "echo", `{"v":"same"}`))
}

// assertFinalizedNonEmpty checks the terminal Event of a forced-finalization run:
// non-empty content (Req#2 — not the prose-less terminalBudgetEvent), NOT
// Escalate-only, and the matching termination_reason/limit_hit observability keys.
func assertFinalizedNonEmpty(t *testing.T, evs []*agent.Event, wantReason string) {
	t.Helper()
	if len(evs) == 0 {
		t.Fatal("no events emitted; want a terminal finalEvent")
	}
	last := evs[len(evs)-1]
	if last.LLMResponse == nil || last.LLMResponse.Content == "" {
		t.Fatalf("terminal event has empty content: %+v (want the non-empty synthesized answer)", last.LLMResponse)
	}
	if last.LLMResponse.Content != finalizeAnswer {
		t.Errorf("terminal content = %q, want the synthesized answer %q", last.LLMResponse.Content, finalizeAnswer)
	}
	if last.Actions.Escalate {
		t.Error("terminal event is Escalate=true; a forced-finalization terminal must be prose, not an Escalate-only signal")
	}
	if got := last.Actions.StateDelta["limit_hit"]; got != wantReason {
		t.Errorf("limit_hit = %v, want %q", got, wantReason)
	}
	if got := last.Actions.StateDelta["termination_reason"]; got != "budget_exhausted" {
		t.Errorf("termination_reason = %v, want budget_exhausted (observability preserved)", got)
	}
}

// countUserNudges returns how many user-role recovery nudges are in the history. The
// original user turn ("ciao") is excluded by matching the recovery-nudge phrasing.
func countUserNudges(hist []llm.Message) int {
	n := 0
	for _, m := range hist {
		if m.Role == llm.RoleUser && strings.Contains(m.Content, "Do not repeat any tool call") {
			n++
		}
	}
	return n
}

// TestFinalize_DedupTrip: a dedup trip now first RECOVERS (one nudge + one more turn,
// Req#3) and only finalizes on the SECOND trip. The recovery turn re-issues the same
// echo call so dedup re-trips; the second trip routes to finalize, which consumes the
// scripted synthesis turn and emits a non-empty finalEvent carrying limit_hit=dedup.
func TestFinalize_DedupTrip(t *testing.T) {
	recordingProvider(t)
	// Three identical echoes trip the window-3 ring (1st trip -> recover). The
	// recovery turn (turn 4) repeats the echo, re-tripping dedup (2nd trip ->
	// finalize). Turn 5 is the tool-free synthesis turn finalize consumes.
	fc := agenttest.NewFakeClient(
		echoTurn(), echoTurn(), echoTurn(), // 3 echoes -> 1st dedup trip
		echoTurn(), // recovery turn re-trips dedup -> 2nd trip
		agenttest.TextChunks("stop", finalizeAnswer), // finalize synthesis
	)
	a := newAgent(t, fc, llm.Config{})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: ptr(50), DedupWindow: ptr(3)})

	evs, err := collect(a.Run(ic))
	if err != nil {
		t.Fatalf("dedup trip surfaced an error slot (must finalize as a terminal Event): %v", err)
	}
	assertFinalizedNonEmpty(t, evs, "dedup")
}

// TestFinalize_MaxStepsTrip: MaxSteps=1 trips max_steps on the second budget gate
// (1st trip -> recover, bypassing the gate). The recovery turn answers via a
// content-stop, so this run ends on the recovery answer; the dedicated finalize-on-
// second-trip proof lives in TestRecovery_TwoTripsOneNudge.
func TestFinalize_MaxStepsTrip(t *testing.T) {
	recordingProvider(t)
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c", "echo", `{"v":"hi"}`)), // budgeted step
		agenttest.TextChunks("stop", finalizeAnswer),                              // recovery turn answers
	)
	a := newAgent(t, fc, llm.Config{})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: ptr(1)})

	evs, err := collect(a.Run(ic))
	if err != nil {
		t.Fatalf("max_steps trip surfaced an error slot: %v", err)
	}
	last := evs[len(evs)-1]
	if last.LLMResponse == nil || last.LLMResponse.Content != finalizeAnswer {
		t.Errorf("terminal content = %+v, want the recovery answer %q", last.LLMResponse, finalizeAnswer)
	}
	if countUserNudges(a.HistoryForTest()) != 1 {
		t.Errorf("want exactly one recovery nudge, got %d", countUserNudges(a.HistoryForTest()))
	}
}

// TestFinalizeWallclock: an injected clock past the deadline trips wallclock on the
// FIRST ConsumeStep (1st trip -> recover via skipBudgetGate). The recovery turn rides
// outside the gate and answers via content-stop. The name MUST match VALIDATION.md's
// `go test -run 'TestFinalizeWallclock'`.
func TestFinalizeWallclock(t *testing.T) {
	recordingProvider(t)
	fc := agenttest.NewFakeClient(agenttest.TextChunks("stop", finalizeAnswer))
	a := newAgent(t, fc, llm.Config{})

	base := time.Now()
	calls := 0
	clock := func() time.Time {
		calls++
		if calls == 1 {
			return base // construction anchor
		}
		return base.Add(2 * time.Hour) // every ConsumeStep check is past the deadline
	}
	b, err := agent.NewBudget(agent.BudgetOptions{MaxWallclockSec: ptr(1), Now: clock})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	ic := agent.InvocationContext{Ctx: context.Background(), RequestID: uuid.Must(uuid.NewV7()), Budget: b}

	evs, rerr := collect(a.Run(ic))
	if rerr != nil {
		t.Fatalf("wallclock trip surfaced an error slot: %v", rerr)
	}
	last := evs[len(evs)-1]
	if last.LLMResponse == nil || last.LLMResponse.Content != finalizeAnswer {
		t.Errorf("terminal content = %+v, want the recovery answer", last.LLMResponse)
	}
}

// TestRecovery_TwoTripsOneNudge (Req#3): two dedup trips inject EXACTLY ONE user-role
// nudge (counter-gated, no second nudge), and the second trip routes to finalize —
// the intended two-trips-one-nudge path (RESEARCH Open Question #2).
func TestRecovery_TwoTripsOneNudge(t *testing.T) {
	recordingProvider(t)
	fc := agenttest.NewFakeClient(
		echoTurn(), echoTurn(), echoTurn(), // 3 echoes -> 1st dedup trip (recover)
		echoTurn(), // recovery turn re-trips dedup -> 2nd trip (finalize)
		agenttest.TextChunks("stop", finalizeAnswer), // finalize synthesis
	)
	a := newAgent(t, fc, llm.Config{})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: ptr(50), DedupWindow: ptr(3)})

	evs, err := collect(a.Run(ic))
	if err != nil {
		t.Fatalf("Run errored: %v", err)
	}
	if got := countUserNudges(a.HistoryForTest()); got != 1 {
		t.Fatalf("recovery nudges = %d, want exactly 1 (counter-gated, no second nudge)", got)
	}
	// The second trip finalized: terminal Event carries the dedup limit_hit.
	assertFinalizedNonEmpty(t, evs, "dedup")
}

// TestFinalizeOutsideBudget (Req#4): recovery + finalize never decrement the step
// counter. With MaxSteps=N, after N budgeted tool turns the budget trips; recovery +
// finalize ride outside it, so Remaining() drops by EXACTLY N and the client is
// called at most N+2 times (RESEARCH §3 AC6 no-production-change assertion).
func TestFinalizeOutsideBudget(t *testing.T) {
	recordingProvider(t)
	const maxSteps = 3
	// N distinct-arg tool turns (no dedup), then a recovery turn that answers, kept
	// long enough that an off-by-one in the ceiling would over-call the client.
	turns := make([]agenttest.FakeTurn, 0, maxSteps+3)
	for i := 0; i < maxSteps; i++ {
		turns = append(turns, agenttest.ToolCallTurn(agenttest.MakeToolCall("c", "echo", `{"v":"`+string(rune('a'+i))+`"}`)))
	}
	turns = append(turns, agenttest.TextChunks("stop", finalizeAnswer)) // recovery answer
	turns = append(turns, agenttest.TextChunks("stop", "extra"))        // guard: must NOT be consumed
	fc := agenttest.NewFakeClient(turns...)
	a := newAgent(t, fc, llm.Config{})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: ptr(maxSteps)})

	if _, err := collect(a.Run(ic)); err != nil {
		t.Fatalf("Run errored: %v", err)
	}
	if got := ic.Budget.Remaining(); got != 0 {
		t.Errorf("Remaining() = %d, want 0 (exactly maxSteps=%d consumed; recovery/finalize did NOT decrement)", got, maxSteps)
	}
	if got := fc.CallCount(); got > maxSteps+2 {
		t.Errorf("client called %d times, want <= maxSteps+2 = %d", got, maxSteps+2)
	}
}

// TestFinalizeFallback (Req#5): when finalize's synthesis is empty/errors TWICE it
// emits the deterministic Italian stub digest; when the retry succeeds it emits the
// retried prose (not the stub). Driven via the dedup two-trip path so finalize runs.
func TestFinalizeFallback(t *testing.T) {
	const toolPreview = "echo:{\"v\":\"same\"}"

	// scriptToFinalize returns the 4 turns that drive a dedup recovery + re-trip so
	// finalize runs; the caller appends the finalize synthesis turn(s).
	scriptToFinalize := func() []agenttest.FakeTurn {
		return []agenttest.FakeTurn{echoTurn(), echoTurn(), echoTurn(), echoTurn()}
	}

	t.Run("empty_then_error_emits_stub", func(t *testing.T) {
		recordingProvider(t)
		turns := append(scriptToFinalize(),
			agenttest.TextChunks("stop"),                // finalize attempt 1: empty content
			agenttest.FakeTurn{Err: errors.New("boom")}, // finalize attempt 2: transport error
		)
		fc := agenttest.NewFakeClient(turns...)
		a := newAgent(t, fc, llm.Config{})
		ic := newIC(t, agent.BudgetOptions{MaxSteps: ptr(50), DedupWindow: ptr(3)})

		evs, err := collect(a.Run(ic))
		if err != nil {
			t.Fatalf("fallback path must not surface an error slot: %v", err)
		}
		last := evs[len(evs)-1]
		if last.LLMResponse == nil || !strings.HasPrefix(last.LLMResponse.Content, stubLeadIn) {
			t.Fatalf("terminal content = %q, want the D-09 stub lead-in prefix", contentOf(last))
		}
		if !strings.Contains(last.LLMResponse.Content, "- echo: "+toolPreview) {
			t.Errorf("stub missing the per-RoleTool bullet; got:\n%s", last.LLMResponse.Content)
		}
	})

	t.Run("empty_then_success_emits_retried_prose", func(t *testing.T) {
		recordingProvider(t)
		const retried = "Risposta al secondo tentativo."
		turns := append(scriptToFinalize(),
			agenttest.TextChunks("stop"),          // finalize attempt 1: empty content
			agenttest.TextChunks("stop", retried), // finalize attempt 2: succeeds
		)
		fc := agenttest.NewFakeClient(turns...)
		a := newAgent(t, fc, llm.Config{})
		ic := newIC(t, agent.BudgetOptions{MaxSteps: ptr(50), DedupWindow: ptr(3)})

		evs, err := collect(a.Run(ic))
		if err != nil {
			t.Fatalf("Run errored: %v", err)
		}
		last := evs[len(evs)-1]
		if last.LLMResponse == nil || last.LLMResponse.Content != retried {
			t.Errorf("terminal content = %q, want the retried prose %q (not the stub)", contentOf(last), retried)
		}
		if strings.HasPrefix(last.LLMResponse.Content, stubLeadIn) {
			t.Error("emitted the stub on retry-success; want the retried prose")
		}
	})
}

// contentOf is a nil-safe accessor for an Event's response content.
func contentOf(ev *agent.Event) string {
	if ev == nil || ev.LLMResponse == nil {
		return ""
	}
	return ev.LLMResponse.Content
}

// hangingClient delegates its first `passthrough` Stream calls to an embedded
// FakeClient (driving the dedup two-trip path into finalize) and then HANGS every
// subsequent call — the finalize synthesis turns — yielding nothing until the
// ctx passed to Stream is cancelled, at which point it closes the channel. It
// models a provider stream that accepts the request but never sends a body.
type hangingClient struct {
	fake        *agenttest.FakeClient
	passthrough int

	mu    sync.Mutex
	calls int
}

func (h *hangingClient) Stream(ctx context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	h.mu.Lock()
	n := h.calls
	h.calls++
	h.mu.Unlock()
	if n < h.passthrough {
		return h.fake.Stream(ctx, req)
	}
	out := make(chan llm.Chunk)
	go func() {
		defer close(out)
		<-ctx.Done() // a hung stream: nothing until the caller's deadline fires
	}()
	return out, nil
}

// TestFinalizeHangingStreamReachesStub is the WR-01 regression: in production
// ic.Ctx = context.Background() (no deadline — cmd/aura/agent.go), so a hung
// provider stream during finalize must NOT block forever. synthesize() bounds
// each attempt with cfg.TotalTimeoutSec; both attempts time out, and the
// deterministic Italian stub is still emitted — the always-return-an-answer
// guarantee holds even when the provider never sends a finalize body. Without the
// per-attempt timeout this test deadlocks (goleak/-timeout would catch it).
func TestFinalizeHangingStreamReachesStub(t *testing.T) {
	recordingProvider(t)
	// 4 echoes drive the dedup two-trip path into finalize; calls 5+ (the finalize
	// synthesis attempt + its retry) hang until their bounded ctx fires.
	hc := &hangingClient{
		fake:        agenttest.NewFakeClient(echoTurn(), echoTurn(), echoTurn(), echoTurn()),
		passthrough: 4,
	}
	// Constructed directly (not via newAgent, which is *FakeClient-typed) so the
	// custom hanging llm.Client can drive finalize. TotalTimeoutSec=1 bounds each
	// stalled synthesis attempt to ~1s.
	a := agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:     hc,
		LLM:        llm.Config{Model: "test-model", Provider: "test-provider", TotalTimeoutSec: 1},
		Registry:   testRegistry(),
		PreviewCap: 2048,
		RunDir:     t.TempDir(),
		SessionID:  uuid.Must(uuid.NewV7()).String(),
		UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: "ciao"}},
	})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: ptr(50), DedupWindow: ptr(3)})

	done := make(chan struct{})
	var evs []*agent.Event
	var runErr error
	go func() {
		defer close(done)
		evs, runErr = collect(a.Run(ic))
	}()

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("finalize hung on a stalled provider stream — WR-01 regression (per-attempt timeout missing)")
	}

	if runErr != nil {
		t.Fatalf("hung-stream finalize must not surface an error slot: %v", runErr)
	}
	last := evs[len(evs)-1]
	if last.LLMResponse == nil || !strings.HasPrefix(last.LLMResponse.Content, stubLeadIn) {
		t.Fatalf("terminal content = %q, want the D-09 stub lead-in (stub must be reachable when finalize stalls)", contentOf(last))
	}
}
