package agui

import (
	"context"
	"iter"
	"runtime"
	"testing"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/chetto1983/aura/internal/agent"
	"go.uber.org/goleak"
)

// eventSource builds a synthetic translated stream: RUN_STARTED, n TEXT_MESSAGE_CONTENT
// deltas under one message run, then RUN_FINISHED. It exercises Fanout the same way the
// real Translate output does (an iter.Seq2[events.Event, error]).
func eventSource(threadID, runID string, n int) iter.Seq2[events.Event, error] {
	return func(yield func(events.Event, error) bool) {
		if !yield(events.NewRunStartedEvent(threadID, runID), nil) {
			return
		}
		const msgID = "msg-1"
		if !yield(events.NewTextMessageStartEvent(msgID, events.WithRole("assistant")), nil) {
			return
		}
		for i := 0; i < n; i++ {
			if !yield(events.NewTextMessageContentEvent(msgID, "x"), nil) {
				return
			}
		}
		if !yield(events.NewTextMessageEndEvent(msgID), nil) {
			return
		}
		yield(events.NewRunFinishedEventWithOptions(threadID, runID, events.WithSuccessOutcome()), nil)
	}
}

// blockingSource yields a first event then blocks until ctx is cancelled, modelling a
// long-running agent turn whose producer goroutine must exit on cancel (Pitfall 4).
func blockingSource(ctx context.Context, first events.Event) iter.Seq2[events.Event, error] {
	return func(yield func(events.Event, error) bool) {
		if !yield(first, nil) {
			return
		}
		<-ctx.Done()
	}
}

func drain(ch <-chan events.Event) []events.Event {
	var out []events.Event
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

// TestFanoutDistributesToAllSubscribers: K subscribers that all read receive every event
// in order, and the producer completes (all channels close) without deadlock.
func TestFanoutDistributesToAllSubscribers(t *testing.T) {
	const n = 5
	f := NewFanout(eventSource("thread-1", "run-1", n))
	subA := f.Subscribe()
	subB := f.Subscribe()
	subC := f.Subscribe()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.Run(ctx)

	want := n + 4 // RUN_STARTED + MSG_START + n CONTENT + MSG_END + RUN_FINISHED
	for name, ch := range map[string]<-chan events.Event{"A": subA, "B": subB, "C": subC} {
		got := drain(ch)
		if len(got) != want {
			t.Fatalf("subscriber %s got %d events, want %d (%v)", name, len(got), want, typesOf(got))
		}
		if string(got[0].Type()) != "RUN_STARTED" {
			t.Fatalf("subscriber %s first = %s, want RUN_STARTED", name, got[0].Type())
		}
		if string(got[len(got)-1].Type()) != "RUN_FINISHED" {
			t.Fatalf("subscriber %s last = %s, want RUN_FINISHED", name, got[len(got)-1].Type())
		}
	}
}

// TestFanoutSlowSubscriberDropped: one never-reading subscriber fills its cap-64 buffer;
// the producer never blocks, a concurrently-draining subscriber receives every event,
// and the run completes. The slow subscriber's overflow is dropped (WARN per drop).
//
// The reader is drained in a goroutine started before Run so it keeps pace and never
// overflows (the realistic concurrent-consumer usage); a sequential post-Run drain
// would itself be subject to drop on a slow machine, masking the actual invariant.
func TestFanoutSlowSubscriberDropped(t *testing.T) {
	// goleak.VerifyTestMain in main_test.go covers the package; the producer goroutine
	// must still exit when the source ends.
	const n = 256 // well past cap-64 so the never-reading subscriber overflows
	f := NewFanout(eventSource("thread-1", "run-1", n))
	reader := f.Subscribe()
	slow := f.Subscribe() // never read → its buffer fills, overflow dropped

	got := make(chan []events.Event, 1)
	go func() { got <- drain(reader) }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.Run(ctx)

	want := n + 4 // RUN_STARTED + MSG_START + n CONTENT + MSG_END + RUN_FINISHED
	var read []events.Event
	select {
	case read = <-got:
	case <-time.After(2 * time.Second):
		t.Fatalf("reader did not complete within 2s — producer blocked on the slow peer?")
	}
	if len(read) != want {
		t.Fatalf("reading subscriber got %d events, want %d — slow peer must not back-pressure", len(read), want)
	}

	// The slow subscriber's channel still closes (sole sender closes on source end),
	// but it received AT MOST its cap-worth — the rest were dropped, not buffered.
	dropped := drain(slow)
	if len(dropped) > fanoutBuffer {
		t.Fatalf("slow subscriber received %d events, want <= cap %d (overflow must be dropped)", len(dropped), fanoutBuffer)
	}
}

// TestFanoutCtxCancelClosesAll: cancelling mid-stream exits the producer and all
// subscriber-feeder work; every subscriber channel closes and goroutines return to
// baseline (Pitfall 4 leak discipline).
func TestFanoutCtxCancelClosesAll(t *testing.T) {
	baseline := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	f := NewFanout(blockingSource(ctx, events.NewRunStartedEvent("thread-1", "run-1")))
	sub := f.Subscribe()
	f.Run(ctx)

	// Confirm the stream is live (first event delivered) before cancelling.
	select {
	case ev, ok := <-sub:
		if !ok {
			t.Fatalf("subscriber channel closed before first event")
		}
		if string(ev.Type()) != "RUN_STARTED" {
			t.Fatalf("first event = %s, want RUN_STARTED", ev.Type())
		}
	case <-time.After(time.Second):
		t.Fatalf("no first event within 1s")
	}

	cancel()

	// The subscriber channel must close (sole sender closes on ctx-cancel).
	select {
	case _, ok := <-sub:
		if ok {
			// drain any buffered remainder, then it must close
			for range sub {
			}
		}
	case <-time.After(time.Second):
		t.Fatalf("subscriber channel did not close within 1s of cancel")
	}

	// Allow the producer goroutine to unwind, then assert no leak.
	deadline := time.Now().Add(time.Second)
	for runtime.NumGoroutine() > baseline+1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > baseline+1 {
		t.Fatalf("goroutines = %d, want <= baseline+1 (%d) — producer leaked on cancel", n, baseline+1)
	}
}

// TestFanoutSourceEndClosesAll: when the wrapped source finishes, every subscriber
// channel closes cleanly even with no cancellation.
func TestFanoutSourceEndClosesAll(t *testing.T) {
	f := NewFanout(eventSource("thread-1", "run-1", 3))
	a := f.Subscribe()
	b := f.Subscribe()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.Run(ctx)

	// Both channels must close on source end; drain returns and ranges terminate.
	if got := len(drain(a)); got == 0 {
		t.Fatalf("subscriber A drained 0 events; want > 0 and a clean close")
	}
	if got := len(drain(b)); got == 0 {
		t.Fatalf("subscriber B drained 0 events; want > 0 and a clean close")
	}
}

func agentSeq(evs []*agent.Event) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		for _, ev := range evs {
			if !yield(ev, nil) {
				return
			}
		}
	}
}

// TestClientSubscriberRoundTrip drives the in-process client subscriber helper with a
// fake agent turn (chunk deltas + a tool call + a final-with-FinishReason) through a
// deterministic IDGenerator, ranges the returned in-process channel, and asserts the
// sequence opens with RUN_STARTED and terminates with RUN_FINISHED.
//
// The channel element type is referenced ONLY through the agui.Event alias (declared as
// the local type Event) — no external events.* symbol appears at this call site, proving
// the alias shields consumers from the SDK package (PRD: avoid leaking the external pkg).
func TestClientSubscriberRoundTrip(t *testing.T) {
	defer goleak.VerifyNone(t)

	turn := []*agent.Event{
		chunk("Ci"), chunk("ao"),
		toolStart("call-1", "web_search", `{"q":"x"}`),
		toolEnd("call-1", "web_search", "preview"),
		finalChunk("Ciao", "stop"),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// drainAlias's <-chan Event parameter pins the channel element type to the agui.Event
	// alias: this call compiles only because Subscribe returns <-chan Event, never
	// <-chan events.Event — proving the alias shields the call site from the SDK package.
	got := drainAlias(Subscribe(ctx, "thread-1", "run-1", &fixedIDGen{}, agentSeq(turn)))
	if len(got) == 0 {
		t.Fatalf("client subscriber yielded no events")
	}
	if string(got[0].Type()) != "RUN_STARTED" {
		t.Fatalf("first event = %s, want RUN_STARTED (%v)", got[0].Type(), aguiTypesOf(got))
	}
	if last := string(got[len(got)-1].Type()); last != "RUN_FINISHED" {
		t.Fatalf("last event = %s, want RUN_FINISHED (%v)", last, aguiTypesOf(got))
	}
	// The tool call must surface through the in-process channel (not just text).
	var sawToolStart bool
	for _, ev := range got {
		if string(ev.Type()) == "TOOL_CALL_START" {
			sawToolStart = true
		}
	}
	if !sawToolStart {
		t.Fatalf("client subscriber dropped the tool call (%v)", aguiTypesOf(got))
	}
}

// drainAlias ranges a subscriber channel typed over the agui.Event alias and collects
// every event. Its <-chan Event parameter is the type-pin that proves Subscribe returns
// the alias, not the SDK's events.Event.
func drainAlias(ch <-chan Event) []Event {
	var out []Event
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

// aguiTypesOf mirrors typesOf but over the agui.Event alias, so the round-trip test
// never names the external events package.
func aguiTypesOf(evs []Event) []string {
	out := make([]string, len(evs))
	for i, ev := range evs {
		out[i] = string(ev.Type())
	}
	return out
}
