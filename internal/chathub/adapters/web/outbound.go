// Package webadapter implements the buffered chathub.OutboundAdapter for
// the synchronous /api/chat request shape. The web SSE streaming adapter
// (Wave 3.0 Slice 4) lives in a sibling file later — today /api/chat is
// a single round-trip JSON endpoint and the adapter buffers events until
// the run reaches a terminal state, then surfaces the final assistant
// content + stats as the response body.
//
// Lifecycle: ONE Router adapter is registered with the Hub at boot. The
// Router maintains a per-RunID Buffer that the ChatService creates on
// dispatch and waits on. This avoids the "growing adapter list" issue of
// per-request adapter registration while keeping each web request's state
// isolated.
package webadapter

import (
	"context"
	"sync"
	"time"

	"github.com/aura/aura/internal/chathub"
)

// Result is the buffered transcript of a single run. Populated by the
// adapter as OutboundEvents arrive; finalised on EventDone / EventError.
// The /api/chat handler converts this into the existing ChatReply JSON
// shape so cmd/chat probes stay byte-identical.
type Result struct {
	FinalContent     string
	Delivered        bool
	Error            string
	Status           string
	LLMCalls         int
	ToolCalls        int
	TokensTotal      int
	TokensPrompt     int
	TokensCompletion int
	CostUSD          float64
	ElapsedMs        int64
	TerminalTool     string

	startedAt time.Time
}

// Buffer collects events for a single run. Created by Router.NewBuffer
// before dispatching the run; Wait blocks until EventDone arrives or ctx
// cancels.
type Buffer struct {
	mu       sync.Mutex
	result   Result
	done     chan struct{}
	finished bool
}

func newBuffer() *Buffer {
	return &Buffer{
		done:   make(chan struct{}),
		result: Result{startedAt: time.Now()},
	}
}

// apply processes one event under the buffer lock. Returns true when the
// buffer reached a terminal state (EventDone).
func (b *Buffer) apply(ev chathub.OutboundEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch ev.Type {
	case chathub.EventMessageDelta:
		if b.result.FinalContent == "" {
			b.result.FinalContent += ev.Content
		}
	case chathub.EventMessageDone:
		if ev.Content != "" {
			b.result.FinalContent = ev.Content
		}
		if d, ok := ev.Payload["delivered"].(bool); ok {
			b.result.Delivered = d
		}
	case chathub.EventUsage:
		if v, ok := ev.Payload["llm_calls"].(int); ok {
			b.result.LLMCalls = v
		}
		if v, ok := ev.Payload["tool_calls"].(int); ok {
			b.result.ToolCalls = v
		}
		if v, ok := ev.Payload["tokens_total"].(int); ok {
			b.result.TokensTotal = v
		}
		if v, ok := ev.Payload["tokens_prompt"].(int); ok {
			b.result.TokensPrompt = v
		}
		if v, ok := ev.Payload["tokens_completion"].(int); ok {
			b.result.TokensCompletion = v
		}
		if v, ok := ev.Payload["cost_usd"].(float64); ok {
			b.result.CostUSD = v
		}
		if v, ok := ev.Payload["terminal_tool"].(string); ok {
			b.result.TerminalTool = v
		}
	case chathub.EventError:
		if v, ok := ev.Payload["error"].(string); ok {
			b.result.Error = v
		}
	case chathub.EventDone:
		if v, ok := ev.Payload["status"].(string); ok {
			b.result.Status = v
		}
		b.result.ElapsedMs = time.Since(b.result.startedAt).Milliseconds()
		if !b.finished {
			b.finished = true
			close(b.done)
		}
	case chathub.EventCancelled:
		b.result.Status = "cancelled"
	}
}

// Wait blocks until the buffer is finalised (EventDone arrives) or ctx
// cancels. Returns the buffered Result.
func (b *Buffer) Wait(ctx context.Context) (Result, error) {
	select {
	case <-b.done:
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.result, nil
	case <-ctx.Done():
		b.mu.Lock()
		defer b.mu.Unlock()
		return b.result, ctx.Err()
	}
}

// Snapshot returns a copy of the current buffered state without waiting.
func (b *Buffer) Snapshot() Result {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.result
}

// Router is the long-lived OutboundAdapter registered once with the Hub.
// Per-request Buffers are reserved via NewBuffer(runID) before dispatch
// and dropped via Drop(runID) once Wait returns.
type Router struct {
	mu      sync.Mutex
	buffers map[string]*Buffer
}

// NewRouter constructs an empty Router. Wire it into the Hub once:
//
//	router := webadapter.NewRouter()
//	hub.RegisterOutbound(router)
func NewRouter() *Router {
	return &Router{buffers: make(map[string]*Buffer)}
}

// Channel + Mode satisfy chathub.OutboundAdapter.
func (*Router) Channel() chathub.Channel   { return chathub.ChannelWeb }
func (*Router) Mode() chathub.DeliveryMode { return chathub.DeliveryModeDeferred }

// Reserve allocates a Buffer for the given RunID. Called by ChatService
// BEFORE Hub dispatch so the buffer is in place when the first event
// arrives. RunID comes from the Run record returned by Hub.ReceiveMessage —
// but ReceiveMessage blocks until run completes, so the buffer must be
// reserved before that call returns. Solution: ChatService inserts an
// anonymous Buffer keyed by a freshly-minted ID via Hub.Bind (see below)
// before ReceiveMessage. Today we use the simpler post-hoc Bind pattern.
//
// Returns a new Buffer; caller is responsible for calling Drop when done.
func (r *Router) Reserve(runID string) *Buffer {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.buffers[runID]
	if !ok {
		b = newBuffer()
		r.buffers[runID] = b
	}
	return b
}

// Drop releases the per-RunID buffer. Call after Wait returns so memory
// doesn't grow unbounded with long-lived bots.
func (r *Router) Drop(runID string) {
	r.mu.Lock()
	delete(r.buffers, runID)
	r.mu.Unlock()
}

// Deliver routes an event to the per-RunID buffer. Events for runs without
// a reserved buffer are dropped (caller-side dispatch hasn't reserved yet,
// or the run is a non-chat-pipe web call we don't care about).
func (r *Router) Deliver(_ context.Context, ev chathub.OutboundEvent) error {
	if ev.RunID == "" {
		return nil
	}
	r.mu.Lock()
	b, ok := r.buffers[ev.RunID]
	if !ok {
		// Lazy-create: a fast Run may emit events before Reserve is
		// called. Better to capture than drop — Reserve will return
		// the same buffer when the caller queries.
		b = newBuffer()
		r.buffers[ev.RunID] = b
	}
	r.mu.Unlock()
	b.apply(ev)
	return nil
}
