// Package pausable provides context deadlines that stop while a human answers.
// A held deadline does not run down, and releasing the hold gives back the time
// it lasted. It exists for MCP elicitation: a server's form waits on the operator
// inside a tool call bounded by the call timeout and by the run's wallclock, and
// neither may count the operator's time.
package pausable

import (
	"sync"
	"time"
)

// Clock accounts for the time a set of deadlines spent held. Every context built
// over one Clock moves with it, and a run's Budget reads the same total, so the
// step gate and the context bounding the run cannot disagree.
type Clock struct {
	now func() time.Time

	mu       sync.Mutex
	holds    int
	heldFrom time.Time
	held     time.Duration
	waiting  map[*deadlineCtx]struct{}
}

// NewClock returns a clock that is not held. now is its time source; nil means
// time.Now.
func NewClock(now func() time.Time) *Clock {
	if now == nil {
		now = time.Now
	}
	return &Clock{now: now, waiting: map[*deadlineCtx]struct{}{}}
}

// Held reports how long the clock has been held in total, an open hold included.
// A nil Clock was never held.
func (c *Clock) Held() time.Duration {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.holds == 0 {
		return c.held
	}
	return c.held + c.now().Sub(c.heldFrom)
}

func (c *Clock) isHeld() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.holds > 0
}

func (c *Clock) hold() (release func()) {
	c.mu.Lock()
	if c.holds == 0 {
		c.heldFrom = c.now()
	}
	c.holds++
	c.mu.Unlock()
	var once sync.Once
	return func() { once.Do(c.release) }
}

func (c *Clock) release() {
	c.mu.Lock()
	c.holds--
	if c.holds > 0 {
		c.mu.Unlock()
		return
	}
	c.held += c.now().Sub(c.heldFrom)
	waiting := make([]*deadlineCtx, 0, len(c.waiting))
	for ctx := range c.waiting {
		waiting = append(waiting, ctx)
	}
	c.mu.Unlock()
	for _, ctx := range waiting {
		ctx.arm()
	}
}

func (c *Clock) watch(ctx *deadlineCtx) {
	c.mu.Lock()
	c.waiting[ctx] = struct{}{}
	c.mu.Unlock()
}

func (c *Clock) forget(ctx *deadlineCtx) {
	c.mu.Lock()
	delete(c.waiting, ctx)
	c.mu.Unlock()
}
