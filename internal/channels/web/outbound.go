// Package webadapter implements the buffered chat.OutboundAdapter for
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

	"github.com/aura/aura/internal/chat"
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
	// ToolsUsed lists the unique tool names invoked during the run, in the
	// order first seen. Captured from EventToolStart events so callers can
	// observe which tools the agent picked without parsing logs. Used by
	// the quality-bench harness to verify tool-selection decisions.
	ToolsUsed []string

	startedAt    time.Time
	toolsUsedSet map[string]struct{}
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
		done: make(chan struct{}),
		result: Result{
			startedAt:    time.Now(),
			toolsUsedSet: make(map[string]struct{}),
		},
	}
}

// apply processes one event under the buffer lock. Returns true when the
// buffer reached a terminal state (EventDone).
func (b *Buffer) apply(ev chat.OutboundEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch ev.Type {
	case chat.EventMessageDelta:
		if b.result.FinalContent == "" {
			b.result.FinalContent += ev.Content
		}
	case chat.EventMessageDone:
		if ev.Content != "" {
			b.result.FinalContent = ev.Content
		}
		if d, ok := ev.Payload["delivered"].(bool); ok {
			b.result.Delivered = d
		}
	case chat.EventToolStart:
		if name, ok := ev.Payload["tool"].(string); ok && name != "" {
			if _, seen := b.result.toolsUsedSet[name]; !seen {
				b.result.toolsUsedSet[name] = struct{}{}
				b.result.ToolsUsed = append(b.result.ToolsUsed, name)
			}
		}
	case chat.EventUsage:
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
	case chat.EventError:
		if v, ok := ev.Payload["error"].(string); ok {
			b.result.Error = v
		}
	case chat.EventDone:
		if v, ok := ev.Payload["status"].(string); ok {
			b.result.Status = v
		}
		b.result.ElapsedMs = time.Since(b.result.startedAt).Milliseconds()
		if !b.finished {
			b.finished = true
			close(b.done)
		}
	case chat.EventCancelled:
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

var _ chat.OutboundAdapter = (*Router)(nil)

// NewRouter constructs an empty Router. Wire it into the Hub once:
//
//	router := webadapter.NewRouter()
//	hub.RegisterOutbound(router)
func NewRouter() *Router {
	return &Router{buffers: make(map[string]*Buffer)}
}

// Channel + Mode satisfy chat.OutboundAdapter.
func (*Router) Channel() chat.Channel   { return chat.ChannelWeb }
func (*Router) Mode() chat.DeliveryMode { return chat.DeliveryModeDeferred }

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
func (r *Router) Deliver(_ context.Context, ev chat.OutboundEvent) error {
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
