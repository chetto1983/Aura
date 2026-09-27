package pausable

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeNow struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeNow() *fakeNow {
	return &fakeNow{t: time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)}
}

func (f *fakeNow) now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

func (f *fakeNow) advance(d time.Duration) {
	f.mu.Lock()
	f.t = f.t.Add(d)
	f.mu.Unlock()
}

func deadlineOf(t *testing.T, ctx context.Context) time.Time {
	t.Helper()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("a pausable context must report a deadline")
	}
	return deadline
}

type plainKey struct{}

func TestWithTimeoutExpiresLikeAStandardDeadline(t *testing.T) {
	ctx, cancel := WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	child, cancelChild := context.WithCancel(ctx)
	defer cancelChild()

	select {
	case <-child.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the deadline never fired")
	}
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", ctx.Err())
	}
	if !errors.Is(child.Err(), context.DeadlineExceeded) {
		t.Fatalf("child err = %v, want DeadlineExceeded: timeout classification reads derived contexts", child.Err())
	}
}

func TestAHeldDeadlineDoesNotRunDown(t *testing.T) {
	clock := newFakeNow()
	c := NewClock(clock.now)
	start := clock.now()
	ctx, cancel := WithDeadline(context.Background(), start.Add(10*time.Second), c)
	defer cancel()

	release := Hold(ctx)
	clock.advance(30 * time.Second)
	if got := deadlineOf(t, ctx); !got.Equal(start.Add(40 * time.Second)) {
		t.Fatalf("deadline during a 30s hold = start+%v, want start+40s", got.Sub(start))
	}
	if err := ctx.Err(); err != nil {
		t.Fatalf("a held context ended: %v", err)
	}
	release()
	if got := c.Held(); got != 30*time.Second {
		t.Fatalf("held = %v, want 30s", got)
	}
	if got := deadlineOf(t, ctx); !got.Equal(start.Add(40 * time.Second)) {
		t.Fatalf("deadline after release = start+%v, want start+40s", got.Sub(start))
	}
}

func TestAReleasedClockPastItsDeadlineExpires(t *testing.T) {
	clock := newFakeNow()
	c := NewClock(clock.now)
	ctx, cancel := WithDeadline(context.Background(), clock.now().Add(time.Second), c)
	defer cancel()

	release := Hold(ctx)
	clock.advance(time.Minute)
	release()
	if err := ctx.Err(); err != nil {
		t.Fatalf("expired although the whole minute was held: %v", err)
	}
	clock.advance(2 * time.Second)
	Hold(ctx)()
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("err = %v one second past the pushed-back deadline, want DeadlineExceeded", ctx.Err())
	}
}

func TestHoldStartingDuringExpiryCheckPreventsExpiry(t *testing.T) {
	manual := newFakeNow()
	var pauseNext atomic.Bool
	entered := make(chan struct{})
	resume := make(chan struct{})
	c := NewClock(func() time.Time {
		if pauseNext.CompareAndSwap(true, false) {
			close(entered)
			<-resume
		}
		return manual.now()
	})
	ctx, cancel := WithDeadline(context.Background(), manual.now().Add(time.Second), c)
	defer cancel()
	manual.advance(2 * time.Second)
	pauseNext.Store(true)
	armed := make(chan struct{})
	go func() {
		ctx.(*deadlineCtx).arm()
		close(armed)
	}()
	<-entered
	release := Hold(ctx)
	close(resume)
	<-armed
	if err := ctx.Err(); err != nil {
		t.Fatalf("context expired while a hold had started: %v", err)
	}
	release()
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("err after release = %v, want DeadlineExceeded", ctx.Err())
	}
}

func TestNestedHoldsRunAgainOnlyAfterTheLast(t *testing.T) {
	clock := newFakeNow()
	c := NewClock(clock.now)
	ctx, cancel := WithDeadline(context.Background(), clock.now().Add(time.Hour), c)
	defer cancel()

	outer := Hold(ctx)
	inner := Hold(ctx)
	clock.advance(5 * time.Second)
	inner()
	clock.advance(5 * time.Second)
	if got := c.Held(); got != 10*time.Second {
		t.Fatalf("held = %v while the outer hold is open, want 10s", got)
	}
	outer()
	outer()
	clock.advance(5 * time.Second)
	if got := c.Held(); got != 10*time.Second {
		t.Fatalf("held = %v after the last release, want 10s", got)
	}
}

func TestADeadlineIsNeverShortened(t *testing.T) {
	clock := newFakeNow()
	c := NewClock(clock.now)
	ctx, cancel := WithDeadline(context.Background(), clock.now().Add(time.Hour), c)
	defer cancel()

	last := deadlineOf(t, ctx)
	var open []func()
	for i := range 50 {
		release := Hold(ctx)
		clock.advance(time.Duration(i%7) * time.Second)
		if i%2 == 0 {
			release()
		} else {
			open = append(open, release)
		}
		clock.advance(time.Second)
		now := deadlineOf(t, ctx)
		if now.Before(last) {
			t.Fatalf("step %d: deadline moved back from %v to %v", i, last, now)
		}
		last = now
	}
	for _, release := range open {
		release()
	}
}

func TestHoldReachesEveryPausableAncestor(t *testing.T) {
	clock := newFakeNow()
	run := NewClock(clock.now)
	call := NewClock(clock.now)
	runCtx, cancelRun := WithDeadline(context.Background(), clock.now().Add(time.Hour), run)
	defer cancelRun()
	between := context.WithValue(runCtx, plainKey{}, "a plain context between the two")
	callCtx, cancelCall := WithDeadline(between, clock.now().Add(time.Minute), call)
	defer cancelCall()

	release := Hold(callCtx)
	clock.advance(20 * time.Second)
	release()
	if run.Held() != 20*time.Second || call.Held() != 20*time.Second {
		t.Fatalf("held run=%v call=%v, want 20s each", run.Held(), call.Held())
	}
}

func TestHoldWithoutAPausableDeadlineIsANoOp(t *testing.T) {
	release := Hold(context.Background())
	release()
	release()
	var none *Clock
	if none.Held() != 0 {
		t.Fatal("a nil clock was never held")
	}
}

func TestTheEarlierParentDeadlineWins(t *testing.T) {
	parent, cancelParent := context.WithTimeout(context.Background(), time.Minute)
	defer cancelParent()
	ctx, cancel := WithTimeout(parent, time.Hour)
	defer cancel()

	want, _ := parent.Deadline()
	if got := deadlineOf(t, ctx); !got.Equal(want) {
		t.Fatalf("deadline = %v, want the parent's %v", got, want)
	}
}

func TestParentCancellationReachesAPausableChild(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	ctx, cancel := WithTimeout(parent, time.Hour)
	defer cancel()

	cancelParent()
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the parent's cancellation never reached the child")
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("err = %v, want Canceled", ctx.Err())
	}
	if ctx.Value(plainKey{}) != nil {
		t.Fatal("an unknown key must fall through to the parent, which does not hold it")
	}
}

func TestADoneParentEndsTheChildAtOnce(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	cancelParent()
	ctx, cancel := WithTimeout(parent, time.Hour)
	defer cancel()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("err = %v right after WithTimeout on a done parent, want Canceled", ctx.Err())
	}
}

func TestCancelEndsTheContextAndItsChildren(t *testing.T) {
	ctx, cancel := WithTimeout(context.Background(), time.Hour)
	child, cancelChild := context.WithCancel(ctx)
	defer cancelChild()

	cancel()
	cancel()
	<-ctx.Done()
	select {
	case <-child.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the child was never cancelled")
	}
	if !errors.Is(child.Err(), context.Canceled) {
		t.Fatalf("child err = %v, want Canceled", child.Err())
	}
}

func TestAfterFuncStopsBeforeTheEndAndRunsAfterIt(t *testing.T) {
	ctx, cancel := WithTimeout(context.Background(), time.Hour)
	pc := ctx.(*deadlineCtx)

	stopped := pc.AfterFunc(func() { t.Error("a stopped AfterFunc ran") })
	if !stopped() {
		t.Fatal("stop before the end must report that it prevented the call")
	}
	ran := make(chan struct{})
	pc.AfterFunc(func() { close(ran) })
	cancel()
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("the AfterFunc never ran")
	}
	late := make(chan struct{})
	lateStop := pc.AfterFunc(func() { close(late) })
	<-late
	if lateStop() {
		t.Fatal("stop after the end must report false")
	}
}
