package pausable

import (
	"context"
	"sync"
	"time"
)

type ctxKey struct{}

// deadlineCtx implements Context because standard deadlines cannot move, and
// cancelling a standard context would give its children context.Canceled.
type deadlineCtx struct {
	parent   context.Context
	clock    *Clock
	deadline time.Time
	up       *deadlineCtx

	mu     sync.Mutex
	done   chan struct{}
	err    error
	timer  *time.Timer
	detach func() bool
	afters map[*afterFunc]struct{}
}

type afterFunc struct{ f func() }

// WithDeadline returns a copy of parent that ends at deadline plus the time clock
// has been held. Several contexts may share one clock; a nil clock gets its own.
func WithDeadline(parent context.Context, deadline time.Time, clock *Clock) (context.Context, context.CancelFunc) {
	if clock == nil {
		clock = NewClock(nil)
	}
	up, _ := parent.Value(ctxKey{}).(*deadlineCtx)
	c := &deadlineCtx{
		parent:   parent,
		clock:    clock,
		deadline: deadline,
		up:       up,
		done:     make(chan struct{}),
		afters:   map[*afterFunc]struct{}{},
	}
	clock.watch(c)
	detach := context.AfterFunc(parent, func() { c.cancel(parent.Err()) })
	c.mu.Lock()
	c.detach = detach
	c.mu.Unlock()
	if err := parent.Err(); err != nil {
		c.cancel(err)
	} else {
		c.arm()
	}
	return c, func() { c.cancel(context.Canceled) }
}

// WithTimeout is WithDeadline over a fresh clock, d from now.
func WithTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	clock := NewClock(nil)
	return WithDeadline(parent, clock.now().Add(d), clock)
}

// Hold stops every pausable deadline above ctx until release runs. Clocks shared
// by multiple deadlines are held once. release is idempotent.
func Hold(ctx context.Context) (release func()) {
	var releases []func()
	seen := map[*Clock]bool{}
	for c, _ := ctx.Value(ctxKey{}).(*deadlineCtx); c != nil; c = c.up {
		if !seen[c.clock] {
			seen[c.clock] = true
			releases = append(releases, c.clock.hold())
		}
	}
	return func() {
		for _, r := range releases {
			r()
		}
	}
}

func (c *deadlineCtx) ownDeadline() time.Time {
	return c.deadline.Add(c.clock.Held())
}

// arm re-checks the current deadline when a timer fires. A hold may have moved
// that deadline after the timer was first scheduled.
func (c *deadlineCtx) arm() {
	if c.clock.isHeld() {
		return
	}
	now := c.clock.now()
	remaining := c.ownDeadline().Sub(now)
	if remaining <= 0 {
		c.clock.expireAt(c, now)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return
	}
	if c.timer != nil {
		c.timer.Stop()
	}
	c.timer = time.AfterFunc(remaining, c.arm)
}

func (c *deadlineCtx) cancel(err error) {
	ended, ok := c.markEnded(err)
	if ok {
		c.finishEnd(ended)
	}
}

type endState struct {
	detach func() bool
	afters map[*afterFunc]struct{}
}

func (c *deadlineCtx) markEnded(err error) (endState, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return endState{}, false
	}
	c.err = err
	close(c.done)
	if c.timer != nil {
		c.timer.Stop()
	}
	ended := endState{detach: c.detach, afters: c.afters}
	c.afters = nil
	return ended, true
}

func (c *deadlineCtx) finishEnd(ended endState) {
	if ended.detach != nil {
		ended.detach()
	}
	c.clock.forget(c)
	for a := range ended.afters {
		go a.f()
	}
}

// Deadline reports the moved deadline, or an earlier parent deadline.
func (c *deadlineCtx) Deadline() (time.Time, bool) {
	own := c.ownDeadline()
	if parent, ok := c.parent.Deadline(); ok && parent.Before(own) {
		return parent, true
	}
	return own, true
}

func (c *deadlineCtx) Done() <-chan struct{} { return c.done }

func (c *deadlineCtx) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *deadlineCtx) Value(key any) any {
	if key == (ctxKey{}) {
		return c
	}
	return c.parent.Value(key)
}

// AfterFunc lets direct children inherit this context's own error, including
// DeadlineExceeded, without a goroutine for each child.
func (c *deadlineCtx) AfterFunc(f func()) (stop func() bool) {
	a := &afterFunc{f: f}
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		go f()
		return func() bool { return false }
	}
	c.afters[a] = struct{}{}
	c.mu.Unlock()
	return func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		_, pending := c.afters[a]
		delete(c.afters, a)
		return pending
	}
}
