package chat

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- Mocks ------------------------------------------------------------------

type recordingLoop struct {
	emits  []OutboundEvent
	err    error
	mu     sync.Mutex
	finalStatus RunStatus
}

func (r *recordingLoop) Run(_ context.Context, run *Run, _ InboundMessage, emit EmitFn) error {
	if r.err != nil {
		return r.err
	}
	r.mu.Lock()
	for _, ev := range r.emits {
		_ = emit(ev)
	}
	r.mu.Unlock()
	if r.finalStatus != "" {
		run.Status = r.finalStatus
	}
	return nil
}

type fakeInbound struct {
	channel Channel
	out     InboundMessage
	err     error
}

func (f *fakeInbound) Channel() Channel { return f.channel }
func (f *fakeInbound) Normalize(_ context.Context, _ any) (InboundMessage, error) {
	return f.out, f.err
}

type fakeOutbound struct {
	channel Channel
	mode    DeliveryMode
	mu      sync.Mutex
	got     []OutboundEvent
	err     error
}

func (f *fakeOutbound) Channel() Channel    { return f.channel }
func (f *fakeOutbound) Mode() DeliveryMode  { return f.mode }
func (f *fakeOutbound) Deliver(_ context.Context, ev OutboundEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, ev)
	return f.err
}

// --- Tests ------------------------------------------------------------------

func newHub(t *testing.T, loop AgentLoop) *Hub {
	t.Helper()
	h, err := New(Config{Loop: loop})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h
}

func TestNew_RejectsMissingLoop(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("expected error when AgentLoop nil")
	}
}

func TestReceive_HappyPath_FanoutsToMatchingOutbound(t *testing.T) {
	loop := &recordingLoop{
		emits: []OutboundEvent{
			{Type: EventMessageCreated},
			{Type: EventMessageDelta, Content: "hello"},
			{Type: EventMessageDone, Content: "hello"},
			{Type: EventUsage, Payload: map[string]any{"tokens": 12}},
		},
	}
	h := newHub(t, loop)

	in := &fakeInbound{channel: ChannelWeb, out: InboundMessage{
		ID: "msg-1", Channel: ChannelWeb, PrincipalID: "p1", Text: "hi",
		Mode: DeliveryModeDeferred, CreatedAt: time.Now(),
	}}
	out := &fakeOutbound{channel: ChannelWeb, mode: DeliveryModeDeferred}
	h.RegisterInbound(in)
	h.RegisterOutbound(out)

	run, err := h.Receive(context.Background(), ChannelWeb, nil)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if run.Status != RunStatusCompleted {
		t.Fatalf("Status = %s, want completed", run.Status)
	}
	// Expected events: run_started + loop's 4 + done = 6
	if got := len(out.got); got != 6 {
		t.Fatalf("delivered %d events, want 6: %+v", got, out.got)
	}
	// First event is run_started (injected by Hub), last is done.
	if out.got[0].Type != EventRunStarted {
		t.Fatalf("first event = %s, want run_started", out.got[0].Type)
	}
	if out.got[len(out.got)-1].Type != EventDone {
		t.Fatalf("last event = %s, want done", out.got[len(out.got)-1].Type)
	}
	// Seq is monotonic + non-zero.
	for i, ev := range out.got {
		if ev.Seq == 0 {
			t.Fatalf("event[%d] has zero Seq", i)
		}
		if i > 0 && ev.Seq <= out.got[i-1].Seq {
			t.Fatalf("seq not monotonic at i=%d: %d <= %d", i, ev.Seq, out.got[i-1].Seq)
		}
	}
	// run_id, channel, created_at populated on every event.
	for i, ev := range out.got {
		if ev.RunID == "" {
			t.Fatalf("event[%d] missing RunID", i)
		}
		if ev.Channel != ChannelWeb {
			t.Fatalf("event[%d] channel=%s, want web", i, ev.Channel)
		}
		if ev.CreatedAt.IsZero() {
			t.Fatalf("event[%d] missing CreatedAt", i)
		}
	}
}

func TestReceive_MissingInboundAdapter(t *testing.T) {
	h := newHub(t, &recordingLoop{})
	_, err := h.Receive(context.Background(), ChannelTelegram, nil)
	if err == nil {
		t.Fatal("expected error for unregistered channel")
	}
}

func TestReceive_InboundNormalizeError(t *testing.T) {
	h := newHub(t, &recordingLoop{})
	h.RegisterInbound(&fakeInbound{channel: ChannelWeb, err: errors.New("bad payload")})
	_, err := h.Receive(context.Background(), ChannelWeb, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReceive_LoopError_PropagatesAndEmitsErrorThenDone(t *testing.T) {
	loop := &recordingLoop{err: errors.New("boom")}
	h := newHub(t, loop)
	h.RegisterInbound(&fakeInbound{channel: ChannelWeb, out: InboundMessage{Channel: ChannelWeb, Mode: DeliveryModeDeferred}})
	out := &fakeOutbound{channel: ChannelWeb, mode: DeliveryModeDeferred}
	h.RegisterOutbound(out)

	run, err := h.Receive(context.Background(), ChannelWeb, nil)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("expected boom, got %v", err)
	}
	if run.Status != RunStatusFailed {
		t.Fatalf("status = %s, want failed", run.Status)
	}
	if run.LastError != "boom" {
		t.Fatalf("LastError = %q", run.LastError)
	}
	// Expect: run_started + error + done.
	if len(out.got) != 3 {
		t.Fatalf("delivered %d events, want 3", len(out.got))
	}
	if out.got[0].Type != EventRunStarted {
		t.Fatalf("evt[0] = %s", out.got[0].Type)
	}
	if out.got[1].Type != EventError {
		t.Fatalf("evt[1] = %s", out.got[1].Type)
	}
	if out.got[2].Type != EventDone {
		t.Fatalf("evt[2] = %s", out.got[2].Type)
	}
}

func TestReceive_ContextCanceled_RunStatusCancelled(t *testing.T) {
	canceledLoop := &recordingLoop{err: context.Canceled}
	h := newHub(t, canceledLoop)
	h.RegisterInbound(&fakeInbound{channel: ChannelWeb, out: InboundMessage{Channel: ChannelWeb, Mode: DeliveryModeDeferred}})
	out := &fakeOutbound{channel: ChannelWeb, mode: DeliveryModeDeferred}
	h.RegisterOutbound(out)

	run, err := h.Receive(context.Background(), ChannelWeb, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if run.Status != RunStatusCancelled {
		t.Fatalf("status = %s, want cancelled", run.Status)
	}
}

func TestStop_BestEffortAndIdempotent(t *testing.T) {
	// Loop that blocks until ctx is done, so Stop can interrupt it.
	type blockingLoop struct{}
	bl := blockingLoopFn(func(ctx context.Context, run *Run, _ InboundMessage, _ EmitFn) error {
		<-ctx.Done()
		return ctx.Err()
	})
	h := newHub(t, bl)
	h.RegisterInbound(&fakeInbound{channel: ChannelWeb, out: InboundMessage{Channel: ChannelWeb, Mode: DeliveryModeDeferred}})
	h.RegisterOutbound(&fakeOutbound{channel: ChannelWeb, mode: DeliveryModeDeferred})

	// Capture the run_id by intercepting the run_started event from a
	// closure-based outbound.
	var runID atomic.Value
	out := outboundFn(ChannelWeb, DeliveryModeDeferred, func(ev OutboundEvent) error {
		if ev.Type == EventRunStarted {
			runID.Store(ev.RunID)
		}
		return nil
	})
	h.RegisterOutbound(out)

	done := make(chan struct{})
	go func() {
		_, _ = h.Receive(context.Background(), ChannelWeb, nil)
		close(done)
	}()

	// Wait for run_id to be populated.
	for i := 0; i < 200; i++ {
		if runID.Load() != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	id, _ := runID.Load().(string)
	if id == "" {
		t.Fatal("never observed run_started")
	}

	if !h.Stop(id) {
		t.Fatal("Stop(known id) returned false")
	}
	// Idempotent: stopping a finished run returns false (already cleaned up).
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("loop did not exit after Stop")
	}
	if h.Stop(id) {
		t.Fatal("second Stop should be false (run already cleaned up)")
	}
	if h.Stop("unknown") {
		t.Fatal("Stop(unknown) should be false")
	}
}

func TestReceiveMessage_BypassesInboundAdapter(t *testing.T) {
	loop := &recordingLoop{emits: []OutboundEvent{{Type: EventMessageDone}}}
	h := newHub(t, loop)
	out := &fakeOutbound{channel: ChannelCron, mode: DeliveryModeSilent}
	h.RegisterOutbound(out)
	// No inbound adapter registered for cron.

	msg := InboundMessage{Channel: ChannelCron, PrincipalID: "system", Mode: DeliveryModeSilent, Text: "tick"}
	_, err := h.ReceiveMessage(context.Background(), msg)
	if err != nil {
		t.Fatalf("ReceiveMessage: %v", err)
	}
	if len(out.got) < 2 {
		t.Fatalf("expected at least run_started + done events, got %d", len(out.got))
	}
}

func TestRegisterOutbound_MultipleAdaptersFanout(t *testing.T) {
	loop := &recordingLoop{emits: []OutboundEvent{{Type: EventMessageDone}}}
	h := newHub(t, loop)
	h.RegisterInbound(&fakeInbound{channel: ChannelTelegram, out: InboundMessage{Channel: ChannelTelegram, Mode: DeliveryModeStreaming}})
	a := &fakeOutbound{channel: ChannelTelegram, mode: DeliveryModeStreaming}
	b := &fakeOutbound{channel: ChannelTelegram, mode: DeliveryModeStreaming}
	h.RegisterOutbound(a)
	h.RegisterOutbound(b)
	if _, err := h.Receive(context.Background(), ChannelTelegram, nil); err != nil {
		t.Fatal(err)
	}
	// Both adapters should have received the same events.
	if len(a.got) != len(b.got) || len(a.got) < 3 {
		t.Fatalf("fanout mismatch: a=%d b=%d", len(a.got), len(b.got))
	}
}

// --- Helpers ---------------------------------------------------------------

type blockingLoopFn func(context.Context, *Run, InboundMessage, EmitFn) error

func (f blockingLoopFn) Run(ctx context.Context, run *Run, msg InboundMessage, emit EmitFn) error {
	return f(ctx, run, msg, emit)
}

type outboundFnAdapter struct {
	ch   Channel
	md   DeliveryMode
	fn   func(OutboundEvent) error
}

func (a outboundFnAdapter) Channel() Channel                                    { return a.ch }
func (a outboundFnAdapter) Mode() DeliveryMode                                  { return a.md }
func (a outboundFnAdapter) Deliver(_ context.Context, ev OutboundEvent) error  { return a.fn(ev) }

func outboundFn(ch Channel, md DeliveryMode, fn func(OutboundEvent) error) OutboundAdapter {
	return outboundFnAdapter{ch: ch, md: md, fn: fn}
}
