package testhelpers

import (
	"context"
	"sync"

	"github.com/aura/aura/internal/chat"
)

// RecordingLoop is an AgentLoop test double that records call counts and emits
// a fixed set of events.
type RecordingLoop struct {
	Emits       []chat.OutboundEvent
	Err         error
	Mu          sync.Mutex
	Calls       int
	FinalStatus chat.RunStatus
}

func (r *RecordingLoop) Run(_ context.Context, run *chat.Run, _ chat.InboundMessage, emit chat.EmitFn) error {
	r.Mu.Lock()
	r.Calls++
	emits := append([]chat.OutboundEvent(nil), r.Emits...)
	err := r.Err
	finalStatus := r.FinalStatus
	r.Mu.Unlock()

	if err != nil {
		return err
	}
	for _, ev := range emits {
		_ = emit(ev)
	}
	if finalStatus != "" {
		run.Status = finalStatus
	}
	return nil
}

// FakeInbound is an InboundAdapter test double.
type FakeInbound struct {
	Ch  chat.Channel
	Out chat.InboundMessage
	Err error
}

func (f *FakeInbound) Channel() chat.Channel { return f.Ch }
func (f *FakeInbound) Normalize(_ context.Context, _ any) (chat.InboundMessage, error) {
	return f.Out, f.Err
}

// FakeOutbound is an OutboundAdapter test double that records delivered events.
type FakeOutbound struct {
	Ch  chat.Channel
	Md  chat.DeliveryMode
	Mu  sync.Mutex
	Got []chat.OutboundEvent
	Err error
}

func (f *FakeOutbound) Channel() chat.Channel   { return f.Ch }
func (f *FakeOutbound) Mode() chat.DeliveryMode { return f.Md }
func (f *FakeOutbound) Deliver(_ context.Context, ev chat.OutboundEvent) error {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	f.Got = append(f.Got, ev)
	return f.Err
}

// BlockingLoopFn is a function adapter that satisfies chat.AgentLoop.
type BlockingLoopFn func(context.Context, *chat.Run, chat.InboundMessage, chat.EmitFn) error

func (f BlockingLoopFn) Run(ctx context.Context, run *chat.Run, msg chat.InboundMessage, emit chat.EmitFn) error {
	return f(ctx, run, msg, emit)
}
