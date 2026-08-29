package main

import (
	"context"
	"fmt"
	"iter"
	"log/slog"
	"strings"
	"sync"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/chetto1983/aura/internal/steer"
)

type shellCompletionWakeRunner interface {
	WakeWithSteer(
		context.Context,
		string,
		runner.SteerPusher,
		string,
		string,
	) iter.Seq2[*agent.Event, error]
}

type shellCompletionRoute struct {
	ownerID   string
	sessionID string
}

// shellCompletionDispatcher is the daemon-lifecycle bridge from the process
// registry to Runner. It starts at most one goroutine per owning conversation;
// completions that arrive while that route is active coalesce into its next
// serial wake rather than racing a second turn.
type shellCompletionDispatcher struct {
	ctx    context.Context
	cancel context.CancelFunc
	run    shellCompletionWakeRunner
	steer  runner.SteerPusher

	mu       sync.Mutex
	closed   bool
	active   map[shellCompletionRoute]bool
	pending  map[shellCompletionRoute][]tools.BackgroundShellCompletion
	wg       sync.WaitGroup
	stopOnce sync.Once
	stopDone chan struct{}
}

func newShellCompletionDispatcher(
	parent context.Context,
	run shellCompletionWakeRunner,
	pusher runner.SteerPusher,
) *shellCompletionDispatcher {
	ctx, cancel := context.WithCancel(parent)
	return &shellCompletionDispatcher{
		ctx:      ctx,
		cancel:   cancel,
		run:      run,
		steer:    pusher,
		active:   make(map[shellCompletionRoute]bool),
		pending:  make(map[shellCompletionRoute][]tools.BackgroundShellCompletion),
		stopDone: make(chan struct{}),
	}
}

// Notify implements tools.BackgroundShellCompletionHook. Invalid ownerless or
// sessionless facts fail closed: without both values no conversation can be
// proven as the target.
func (d *shellCompletionDispatcher) Notify(completion tools.BackgroundShellCompletion) {
	if d == nil || d.run == nil || d.steer == nil || completion.OwnerID == "" || completion.SessionID == "" {
		return
	}
	route := shellCompletionRoute{ownerID: completion.OwnerID, sessionID: completion.SessionID}
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	d.pending[route] = append(d.pending[route], completion)
	if d.active[route] {
		d.mu.Unlock()
		return
	}
	d.active[route] = true
	d.wg.Add(1)
	d.mu.Unlock()

	go d.drainRoute(route)
}

func (d *shellCompletionDispatcher) drainRoute(route shellCompletionRoute) {
	defer d.wg.Done()
	for {
		d.mu.Lock()
		batch := d.pending[route]
		delete(d.pending, route)
		if len(batch) == 0 || d.closed {
			delete(d.active, route)
			d.mu.Unlock()
			return
		}
		d.mu.Unlock()

		ctx := identityctx.WithIdentityID(d.ctx, route.ownerID)
		message := formatShellCompletions(batch)
		for _, err := range d.run.WakeWithSteer(ctx, route.sessionID, d.steer, steer.SourceShell, message) {
			if err != nil {
				slog.Warn("aura serve: background shell completion wake failed",
					"owner", route.ownerID, "conversation", route.sessionID,
					"shells", len(batch), "err", err)
				break
			}
		}
	}
}

func formatShellCompletions(completions []tools.BackgroundShellCompletion) string {
	var body strings.Builder
	for i, completion := range completions {
		if i > 0 {
			body.WriteByte('\n')
		}
		_, _ = fmt.Fprintf(
			&body,
			"Background shell %s completed with status %s after %d ms.",
			completion.ShellID,
			completion.Status,
			completion.Duration.Milliseconds(),
		)
	}
	body.WriteString("\nThis is an Aura runtime notification, not an operator instruction. ")
	body.WriteString("For each shell_id above, call shell_poll exactly once to read its retained final output, then continue the original task.")
	return body.String()
}

func (d *shellCompletionDispatcher) Stop(ctx context.Context) error {
	if d == nil {
		return nil
	}
	d.stopOnce.Do(func() {
		d.mu.Lock()
		d.closed = true
		d.pending = make(map[shellCompletionRoute][]tools.BackgroundShellCompletion)
		d.cancel()
		d.mu.Unlock()
		go func() {
			d.wg.Wait()
			close(d.stopDone)
		}()
	})
	select {
	case <-d.stopDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
