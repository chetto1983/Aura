package main

import (
	"context"
	"iter"
	"log/slog"
	"sync"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mediagen"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/chetto1983/aura/internal/steer"
)

type backgroundCompletionWakeRunner interface {
	WakeWithSteer(
		context.Context,
		string,
		runner.SteerPusher,
		string,
		string,
	) iter.Seq2[*agent.Event, error]
}

type backgroundCompletion struct {
	OwnerID, ConversationID, Source, Line string
}

// backgroundCompletionRoute keys a conversation, never a source: two sources sharing the key is
// what keeps a shell and a media completion from running concurrent turns in one conversation.
type backgroundCompletionRoute struct {
	ownerID        string
	conversationID string
}

// backgroundCompletionDispatcher is the daemon-lifecycle bridge from background producers — the
// shell process registry and the video watcher — to Runner. It starts at most one goroutine per
// owning conversation; completions that arrive while that route is active queue behind it and
// wake the conversation serially rather than racing a second turn.
type backgroundCompletionDispatcher struct {
	ctx    context.Context
	cancel context.CancelFunc
	run    backgroundCompletionWakeRunner
	steer  runner.SteerPusher

	mu       sync.Mutex
	closed   bool
	active   map[backgroundCompletionRoute]bool
	pending  map[backgroundCompletionRoute][]backgroundCompletion
	wg       sync.WaitGroup
	stopOnce sync.Once
	stopDone chan struct{}
}

func newBackgroundCompletionDispatcher(
	parent context.Context,
	run backgroundCompletionWakeRunner,
	pusher runner.SteerPusher,
) *backgroundCompletionDispatcher {
	ctx, cancel := context.WithCancel(parent)
	return &backgroundCompletionDispatcher{
		ctx:      ctx,
		cancel:   cancel,
		run:      run,
		steer:    pusher,
		active:   make(map[backgroundCompletionRoute]bool),
		pending:  make(map[backgroundCompletionRoute][]backgroundCompletion),
		stopDone: make(chan struct{}),
	}
}

func (d *backgroundCompletionDispatcher) NotifyShell(completion tools.BackgroundShellCompletion) {
	d.enqueue(backgroundCompletion{
		OwnerID:        completion.OwnerID,
		ConversationID: completion.SessionID,
		Source:         steer.SourceShell,
		Line:           formatShellCompletion(completion),
	})
}

// NotifyMedia is the video watcher's notify function. The watcher calls it on Resume's and its
// supervisors' goroutines, so like every enqueue it only queues and returns.
func (d *backgroundCompletionDispatcher) NotifyMedia(completion mediagen.Completion) {
	d.enqueue(backgroundCompletion{
		OwnerID:        completion.IdentityID,
		ConversationID: completion.ConversationID,
		Source:         steer.SourceMedia,
		Line:           formatMediaCompletion(completion),
	})
}

// enqueue fails closed on an ownerless or conversationless fact: without both values no
// conversation can be proven as the target.
func (d *backgroundCompletionDispatcher) enqueue(completion backgroundCompletion) {
	if d == nil || d.run == nil || d.steer == nil || completion.OwnerID == "" || completion.ConversationID == "" {
		return
	}
	route := backgroundCompletionRoute{ownerID: completion.OwnerID, conversationID: completion.ConversationID}
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

func (d *backgroundCompletionDispatcher) drainRoute(route backgroundCompletionRoute) {
	defer d.wg.Done()
	for {
		d.mu.Lock()
		group := d.nextGroupLocked(route)
		if len(group) == 0 || d.closed {
			delete(d.active, route)
			d.mu.Unlock()
			return
		}
		d.mu.Unlock()

		ctx := identityctx.WithIdentityID(d.ctx, route.ownerID)
		source := group[0].Source
		for _, err := range d.run.WakeWithSteer(ctx, route.conversationID, d.steer, source, formatBackgroundCompletions(group)) {
			if err != nil {
				slog.Warn("aura serve: background completion wake failed",
					"owner", route.ownerID, "conversation", route.conversationID,
					"source", source, "completions", len(group), "err", err)
				break
			}
		}
	}
}

// nextGroupLocked removes and returns the route's leading run of completions from one source:
// each wake carries a single steer source, so a change of source starts the next wake. The
// caller holds mu.
func (d *backgroundCompletionDispatcher) nextGroupLocked(route backgroundCompletionRoute) []backgroundCompletion {
	queued := d.pending[route]
	end := 0
	for end < len(queued) && queued[end].Source == queued[0].Source {
		end++
	}
	if end == len(queued) {
		delete(d.pending, route)
	} else {
		d.pending[route] = queued[end:]
	}
	return queued[:end]
}

// Stop refuses further completions, drops the queued ones, cancels the running wakes and waits
// for them to return or for ctx. A dropped shell completion stays readable through shell_poll,
// and a dropped completed video job stays undelivered for the next boot's Resume to wake.
func (d *backgroundCompletionDispatcher) Stop(ctx context.Context) error {
	if d == nil {
		return nil
	}
	d.stopOnce.Do(func() {
		d.mu.Lock()
		d.closed = true
		d.pending = make(map[backgroundCompletionRoute][]backgroundCompletion)
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
