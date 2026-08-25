package runner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/steer"
)

// runner_steer_leftover_test.go carries the 52-05 leftover-steer
// auto-delivery test suite -- split out of runner_steer_test.go (52-04's
// original persistSteerTurn/wire-validity tests stay there) purely for
// the 600-LOC file-size discipline; no behavior split, same package, same
// helpers (steerInjectorTool, assertWireValidHistory, textResponseCall,
// newTestRunner, mustCreate, newConvID, drain -- all defined in sibling
// _test.go files in this package).

// joinRecordedContents concatenates a recorded request's message contents so a
// test can substring-search the round's full text for the notice line / the
// leftover text, regardless of which message carries it.
func joinRecordedContents(msgs []llm.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(m.Content)
	}
	return b.String()
}

// steerDeltaSteers extracts the "steers" slice off an Event's SteerDelta, or
// nil when the Event carries none.
func steerDeltaSteers(ev *agent.Event) []map[string]any {
	if ev == nil || ev.Actions.SteerDelta == nil {
		return nil
	}
	steers, _ := ev.Actions.SteerDelta["steers"].([]map[string]any)
	return steers
}

// TestLeftoverSteerAutoDeliversAsNextTurn closes STEER-04's substantive
// correction (D-09): a steer accepted while a run was live but never reached
// either drain point before the round ended is not returned to the operator
// to retype -- it is delivered automatically as the very next turn, said out
// loud via the visible notice line, and reaches the model.
//
// The push is timed deterministically, no goroutine/sleep involved: it fires
// from INSIDE the test's own yield callback, synchronously, the instant round
// 1's terminal (FinishReason-carrying) Event is observed -- i.e. AFTER the
// agent's own last drain point already ran (a text_response call is
// terminal, so llm_agent.go's dispatch() returns before ever reaching drain
// point B) and BEFORE deliverLeftoverSteer's post-loop drain executes. This
// reproduces the exact race window STEER-04 exists to close without needing
// real HTTP concurrency (mirrors steerInjectorTool's "no goroutine needed"
// precedent above).
func TestLeftoverSteerAutoDeliversAsNextTurn(t *testing.T) {
	inbox := steer.New(steer.Config{Max: 8, MaxBytes: 16384})
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(textResponseCall("call-1", "round one done")),
		agenttest.ToolCallTurn(textResponseCall("call-2", "round two done")),
	)
	r, conv, _ := newTestRunner(t, client)
	r.steer = inbox
	convID := newConvID(t)
	mustCreate(t, r, convID)

	pushed := false
	var evs []*agent.Event
	for ev, err := range r.Turn(context.Background(), convID, new("please handle this task")) {
		if err != nil {
			t.Fatalf("turn error: %v", err)
		}
		evs = append(evs, ev)
		if !pushed && ev.LLMResponse != nil && ev.LLMResponse.FinishReason != "" {
			if err := inbox.Push(convID, "cockpit", "switch to plan B"); err != nil {
				t.Fatalf("Push: %v", err)
			}
			pushed = true
		}
	}
	if !pushed {
		t.Fatal("never observed round 1's terminal event -- the race window this test drives was never opened")
	}

	var notice *agent.Event
	for _, ev := range evs {
		if ev.Actions.SteerDelta != nil {
			notice = ev
		}
	}
	if notice == nil {
		t.Fatalf("no aura.steer notice Event among %d events: %+v", len(evs), evs)
	}
	steers := steerDeltaSteers(notice)
	if len(steers) != 1 {
		t.Fatalf("notice steers = %+v, want exactly 1 entry", steers)
	}
	if got := steers[0]["delivery"]; got != steerDeliveryAutoNextTurn {
		t.Fatalf("notice delivery = %v, want %q (the structural next-turn signal)", got, steerDeliveryAutoNextTurn)
	}
	if got := steers[0]["text"]; got != "switch to plan B" {
		t.Fatalf("notice text = %v, want the RAW leftover text", got)
	}

	reqs := client.RecordedRequests()
	if len(reqs) != 2 {
		t.Fatalf("LLM calls = %d, want 2 (round 1 + the auto-delivered follow-on turn)", len(reqs))
	}
	round2 := joinRecordedContents(reqs[1].Messages)
	if !strings.Contains(round2, steerAutoDeliveryNotice) {
		t.Fatalf("round-2 request missing the visible notice line: %+v", reqs[1].Messages)
	}
	if !strings.Contains(round2, "switch to plan B") {
		t.Fatalf("round-2 request missing the leftover text: %+v", reqs[1].Messages)
	}

	hist, err := conv.LoadHistory(context.Background(), convID)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	count := 0
	for _, m := range hist {
		if m.Role == llm.RoleUser && strings.Contains(m.Content, "switch to plan B") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("persisted RoleUser rows carrying the leftover text = %d, want exactly 1: %+v", count, hist)
	}
}

// TestLeftoverSteerPersistsExactlyOneTurn is the plan's own named deliverable:
// it COUNTS the matching aura.conversation_turns-equivalent rows rather than
// merely asserting one exists, so it fails on the double-write that would
// result if 52-04's drain-time persistSteerTurn branch ALSO fired for the
// next-turn delivery form (T-52-26).
func TestLeftoverSteerPersistsExactlyOneTurn(t *testing.T) {
	inbox := steer.New(steer.Config{Max: 8, MaxBytes: 16384})
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(textResponseCall("call-1", "round one done")),
		agenttest.ToolCallTurn(textResponseCall("call-2", "round two done")),
	)
	r, conv, _ := newTestRunner(t, client)
	r.steer = inbox
	convID := newConvID(t)
	mustCreate(t, r, convID)

	const leftover = "actually, stop and summarize instead"
	pushed := false
	if _, err := drain(func(yield func(*agent.Event, error) bool) {
		for ev, err := range r.Turn(context.Background(), convID, new("go ahead")) {
			if !pushed && ev != nil && ev.LLMResponse != nil && ev.LLMResponse.FinishReason != "" {
				if pushErr := inbox.Push(convID, "cockpit", leftover); pushErr != nil {
					t.Fatalf("Push: %v", pushErr)
				}
				pushed = true
			}
			if !yield(ev, err) {
				return
			}
		}
	}); err != nil {
		t.Fatalf("turn: %v", err)
	}
	if !pushed {
		t.Fatal("race window never opened")
	}

	hist, err := conv.LoadHistory(context.Background(), convID)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	count := 0
	for _, m := range hist {
		if m.Role == llm.RoleUser && strings.Contains(m.Content, leftover) {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("persisted RoleUser rows carrying the leftover text = %d, want EXACTLY 1 (a status code cannot prove this -- only a row count can): %+v", count, hist)
	}
}

// TestLeftoverSteerNoLeftoverPathUnchanged proves the negative: with steering
// wired but NOTHING queued, the turn is byte-identical (same event count,
// same persisted turn count, same number of LLM client calls) to a run with
// no steering at all.
func TestLeftoverSteerNoLeftoverPathUnchanged(t *testing.T) {
	baseline := agenttest.NewFakeClient(agenttest.ToolCallTurn(textResponseCall("call-1", "done")))
	rBase, convBase, _ := newTestRunner(t, baseline)
	convIDBase := newConvID(t)
	mustCreate(t, rBase, convIDBase)
	evsBase, err := drain(rBase.Turn(context.Background(), convIDBase, new("please handle this task")))
	if err != nil {
		t.Fatalf("baseline turn: %v", err)
	}
	histBase, err := convBase.LoadHistory(context.Background(), convIDBase)
	if err != nil {
		t.Fatalf("baseline LoadHistory: %v", err)
	}

	steered := agenttest.NewFakeClient(agenttest.ToolCallTurn(textResponseCall("call-1", "done")))
	rSteered, convSteered, _ := newTestRunner(t, steered)
	rSteered.steer = steer.New(steer.Config{Max: 8, MaxBytes: 16384})
	convIDSteered := newConvID(t)
	mustCreate(t, rSteered, convIDSteered)
	evsSteered, err := drain(rSteered.Turn(context.Background(), convIDSteered, new("please handle this task")))
	if err != nil {
		t.Fatalf("steered turn: %v", err)
	}
	histSteered, err := convSteered.LoadHistory(context.Background(), convIDSteered)
	if err != nil {
		t.Fatalf("steered LoadHistory: %v", err)
	}

	if len(evsBase) != len(evsSteered) {
		t.Fatalf("event count baseline=%d steered=%d, want equal (steering wired but idle must be inert)", len(evsBase), len(evsSteered))
	}
	if len(histBase) != len(histSteered) {
		t.Fatalf("persisted turn count baseline=%d steered=%d, want equal", len(histBase), len(histSteered))
	}
	if baseline.CallCount() != steered.CallCount() {
		t.Fatalf("LLM call count baseline=%d steered=%d, want equal (no extra call from an idle inbox check)", baseline.CallCount(), steered.CallCount())
	}
	for _, ev := range evsSteered {
		if ev.Actions.SteerDelta != nil {
			t.Fatalf("steered-but-idle run emitted a SteerDelta Event: %+v", ev)
		}
	}
}

// TestLeftoverSteerNilInboxIsInert proves the D-12 rollback path: with
// r.steer == nil (AURA_AGUI_RUN_STEER=false), deliverLeftoverSteer's wrapper
// is a total no-op -- it never even calls Drain.
func TestLeftoverSteerNilInboxIsInert(t *testing.T) {
	client := agenttest.NewFakeClient(agenttest.ToolCallTurn(textResponseCall("call-1", "done")))
	r, conv, _ := newTestRunner(t, client)
	// r.steer left nil deliberately -- newTestRunner never wires one.
	convID := newConvID(t)
	mustCreate(t, r, convID)

	evs, err := drain(r.Turn(context.Background(), convID, new("please handle this task")))
	if err != nil {
		t.Fatalf("turn: %v", err)
	}
	for _, ev := range evs {
		if ev.Actions.SteerDelta != nil {
			t.Fatalf("nil-inbox run emitted a SteerDelta Event: %+v", ev)
		}
	}
	hist, err := conv.LoadHistory(context.Background(), convID)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(hist) != 2 {
		t.Fatalf("persisted turns = %d, want 2 (user + assistant, no extra)", len(hist))
	}
}

// TestLeftoverSteerHonoursYieldAfterFalse drives a consumer that stops after
// the FIRST event and asserts deliverLeftoverSteer never yields again --
// honoring the iter.Seq2 contract even though a leftover is genuinely queued
// and would otherwise trigger the auto-delivery notice + follow-on turn.
func TestLeftoverSteerHonoursYieldAfterFalse(t *testing.T) {
	inbox := steer.New(steer.Config{Max: 8, MaxBytes: 16384})
	if err := inbox.Push("conv-stop-early", "cockpit", "never delivered"); err != nil {
		t.Fatalf("Push: %v", err)
	}
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	r.steer = inbox

	calls := 0
	inner := func(yield func(*agent.Event, error) bool) {
		yield(&agent.Event{Author: "aura"}, nil)
	}
	for range r.deliverLeftoverSteer(context.Background(), "conv-stop-early", inner) {
		calls++
		break // consumer stops after the first event
	}
	if calls != 1 {
		t.Fatalf("consumer observed %d events, want exactly 1 (it stopped after the first)", calls)
	}
	// The inbox must be untouched: a consumer that stops early must never see
	// the wrapper reach its post-loop drain.
	if msgs := inbox.Drain("conv-stop-early"); len(msgs) != 1 {
		t.Fatalf("inbox after an early-stopped consumer = %d messages, want the original 1 still queued (never drained)", len(msgs))
	}
}

// TestAutoDeliveryChainIsBounded closes T-52-21: a steer that arrives DURING
// the auto-delivered follow-on turn does not trigger a SECOND auto-delivery
// within the same outer Turn call. steerAutoDeliverMaxChain=1 makes this
// structural; this test proves the observable behavior matches. The
// mid-follow-on push uses the same steerInjectorTool precedent (deterministic,
// no goroutine) -- the injected steer lands during round 2's OWN dispatch,
// which is non-terminal, so round 2's own drain point B (not a second
// auto-delivery hop) is what would normally catch it.
func TestAutoDeliveryChainIsBounded(t *testing.T) {
	inbox := steer.New(steer.Config{Max: 8, MaxBytes: 16384})
	convID := newConvID(t)
	client := agenttest.NewFakeClient(
		// Round 1: terminal call, so the FIRST leftover ("switch to plan B") is
		// pushed by the test driver at round 1's terminal event (below) and
		// never drained internally.
		agenttest.ToolCallTurn(textResponseCall("call-1", "round one done")),
		// Round 2 (the auto-delivered follow-on): a non-terminal tool call that
		// itself pushes a SECOND leftover mid-dispatch -- round 2's OWN drain
		// point B (llm_agent.go) catches it immediately, since it is a
		// non-terminal dispatch, so it is delivered inside round 2, not chased
		// by a second auto-delivery hop.
		agenttest.ToolCallTurn(agenttest.MakeToolCall("call-2", "steer_inject", "{}")),
		// Round 3: round 2's own continuation after its drain point B fires.
		agenttest.ToolCallTurn(textResponseCall("call-3", "round two done")),
	)
	r, conv, _ := newTestRunner(t, client)
	r.steer = inbox
	r.registry.Register(&steerInjectorTool{inbox: inbox, convID: convID, text: "second leftover, mid follow-on"})
	mustCreate(t, r, convID)

	pushed := false
	var evs []*agent.Event
	for ev, err := range r.Turn(context.Background(), convID, new("please handle this task")) {
		if err != nil {
			t.Fatalf("turn error: %v", err)
		}
		evs = append(evs, ev)
		if !pushed && ev.LLMResponse != nil && ev.LLMResponse.FinishReason != "" {
			if pushErr := inbox.Push(convID, "cockpit", "switch to plan B"); pushErr != nil {
				t.Fatalf("Push: %v", pushErr)
			}
			pushed = true
		}
	}
	if !pushed {
		t.Fatal("race window never opened")
	}

	noticeCount := 0
	for _, ev := range evs {
		if ev.Actions.SteerDelta != nil {
			steers := steerDeltaSteers(ev)
			for _, s := range steers {
				if s["delivery"] == steerDeliveryAutoNextTurn {
					noticeCount++
				}
			}
		}
	}
	if noticeCount != 1 {
		t.Fatalf("auto-delivery notices observed = %d, want exactly 1 (the chain cap forbids a second hop)", noticeCount)
	}

	// Both leftovers still reached the model: the first via the follow-on
	// turn's own user message, the second via round 2's normal drain point.
	reqs := client.RecordedRequests()
	if len(reqs) != 3 {
		t.Fatalf("LLM calls = %d, want 3 (round 1 + auto-delivered round 2 + round 2's own continuation)", len(reqs))
	}
	round2 := joinRecordedContents(reqs[1].Messages)
	if !strings.Contains(round2, "switch to plan B") {
		t.Fatalf("round 2 (the auto-delivered follow-on) missing the first leftover: %+v", reqs[1].Messages)
	}
	round3 := joinRecordedContents(reqs[2].Messages)
	if !strings.Contains(round3, "second leftover, mid follow-on") {
		t.Fatalf("round 3 missing the second leftover (round 2's own drain point B should have caught it): %+v", reqs[2].Messages)
	}

	hist, err := conv.LoadHistory(context.Background(), convID)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	assertWireValidHistory(t, hist)
}

// TestDeliverLeftoverSteerRaceLockedContextReuse drives the leftover
// auto-delivery through the PUBLIC Turn entrypoint under -race with the REAL
// per-conversation lock (never a fake): T-52-22's mitigation plan requires
// exactly this -- the follow-on turn must run under the SAME held lock rather
// than attempting a fresh Lock() against a mutex this goroutine already
// holds, which would deadlock instead of merely racing. A deadlock hangs the
// test (caught by the test timeout), not a race-detector finding, so this
// test's real assertion is that it completes at all.
func TestDeliverLeftoverSteerRaceLockedContextReuse(t *testing.T) {
	inbox := steer.New(steer.Config{Max: 8, MaxBytes: 16384})
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(textResponseCall("call-1", "round one done")),
		agenttest.ToolCallTurn(textResponseCall("call-2", "round two done")),
	)
	r, _, _ := newTestRunner(t, client)
	r.steer = inbox
	convID := newConvID(t)
	mustCreate(t, r, convID)

	done := make(chan error, 1)
	go func() {
		_, err := drain(func(yield func(*agent.Event, error) bool) {
			for ev, err := range r.Turn(context.Background(), convID, new("please handle this task")) {
				if ev != nil && ev.LLMResponse != nil && ev.LLMResponse.FinishReason != "" {
					_ = inbox.Push(convID, "cockpit", "switch to plan B")
				}
				if !yield(ev, err) {
					return
				}
			}
		})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("turn: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("turn did not complete within 10s -- suspected deadlock re-acquiring the per-conversation lock")
	}
}
