package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/pausable"
)

// manualClock drives a Budget's injected clock so held time is exact.
type manualClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *manualClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func pausableBudget(t *testing.T, wallclockSec int) (*Budget, *manualClock) {
	t.Helper()
	clock := &manualClock{now: time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)}
	b, err := NewBudget(BudgetOptions{MaxWallclockSec: &wallclockSec, Now: clock.Now})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	return b, clock
}

func TestBudgetWallclockSkipsHeldTime(t *testing.T) {
	b, clock := pausableBudget(t, 60)
	ctx, cancel := b.WithDeadline(context.Background())
	defer cancel()

	release := pausable.Hold(ctx)
	clock.advance(10 * time.Minute)
	if ok, reason := b.ConsumeStep(); !ok {
		t.Fatalf("step refused (%s) while the operator holds the clock", reason)
	}
	release()
	clock.advance(30 * time.Second)
	if ok, reason := b.ConsumeStep(); !ok {
		t.Fatalf("step refused (%s) after 30s of run time and a 10-minute hold, with a 60s wallclock", reason)
	}
	clock.advance(31 * time.Second)
	if ok, reason := b.ConsumeStep(); ok || reason != "wallclock" {
		t.Fatalf("ConsumeStep = %v %q after 61s of run time, want the wallclock refusal", ok, reason)
	}
}

func TestChildBudgetSeesTheParentsHeldTime(t *testing.T) {
	b, clock := pausableBudget(t, 60)
	child := b.Child(2)
	ctx, cancel := b.WithDeadline(context.Background())
	defer cancel()

	release := pausable.Hold(ctx)
	clock.advance(5 * time.Minute)
	release()
	clock.advance(30 * time.Second)
	if ok, reason := child.ConsumeStep(); !ok {
		t.Fatalf("a sub-agent's budget refused a step (%s): it must share the parent's held time", reason)
	}
}

func TestBudgetDeadlineContextMovesWithAHold(t *testing.T) {
	b, clock := pausableBudget(t, 60)
	ctx, cancel := b.WithDeadline(context.Background())
	defer cancel()

	before, _ := ctx.Deadline()
	release := pausable.Hold(ctx)
	clock.advance(2 * time.Minute)
	release()
	after, _ := ctx.Deadline()
	if got := after.Sub(before); got != 2*time.Minute {
		t.Fatalf("the run context's deadline moved by %v, want the 2m hold", got)
	}
}
