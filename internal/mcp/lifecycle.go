package mcp

import (
	"context"
	"fmt"
	"sync"
)

// ErrClientClosed marks an MCP session whose shutdown has started. It wraps
// ErrTransport so reconnecting callers classify it as terminal transport state.
var ErrClientClosed = fmt.Errorf("%w: client closed", ErrTransport)

// sessionGate serializes a request/response stream while letting queued callers
// select on cancellation and shutdown. Its zero value is ready for use.
type sessionGate struct {
	initOnce  sync.Once
	closeOnce sync.Once
	permit    chan struct{}
	closed    chan struct{}

	activeMu     sync.Mutex
	activeCancel context.CancelFunc
}

func (g *sessionGate) init() {
	g.initOnce.Do(func() {
		g.permit = make(chan struct{}, 1)
		g.permit <- struct{}{}
		g.closed = make(chan struct{})
	})
}

func (g *sessionGate) acquire(ctx context.Context) (context.Context, func(), error) {
	g.init()
	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	case <-g.closed:
		return nil, nil, ErrClientClosed
	case <-g.permit:
	}

	g.activeMu.Lock()
	select {
	case <-g.closed:
		g.activeMu.Unlock()
		g.permit <- struct{}{}
		return nil, nil, ErrClientClosed
	default:
	}
	callCtx, cancel := context.WithCancel(ctx)
	g.activeCancel = cancel
	g.activeMu.Unlock()

	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			g.activeMu.Lock()
			g.activeCancel = nil
			g.activeMu.Unlock()
			cancel()
			g.permit <- struct{}{}
		})
	}
	return callCtx, release, nil
}

// beginClose publishes terminal state before cancelling the active request.
func (g *sessionGate) beginClose() {
	g.init()
	g.closeOnce.Do(func() {
		close(g.closed)
		g.activeMu.Lock()
		if g.activeCancel != nil {
			g.activeCancel()
		}
		g.activeMu.Unlock()
	})
}

// waitIdle takes the permit permanently for the idempotent close path.
func (g *sessionGate) waitIdle(ctx context.Context) error {
	g.init()
	select {
	case <-g.permit:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
