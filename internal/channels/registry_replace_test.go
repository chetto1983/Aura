package channels

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// runningGauge counts how many instances sharing it are started at once. Two live
// instances under one name is the failure Replace exists to prevent: two Telegram
// pollers on one token make getUpdates answer 409 Conflict to both.
type runningGauge struct {
	mu      sync.Mutex
	running int
	peak    int
}

// swapChannel is a Deliverer whose Start/Stop move a shared runningGauge. Its Start
// yields for a moment so an unserialised pair of Replace calls would visibly overlap.
type swapChannel struct {
	fakeDeliverer
	gauge *runningGauge
}

func (c *swapChannel) Start(ctx context.Context) error {
	if err := c.fakeDeliverer.Start(ctx); err != nil {
		return err
	}
	c.gauge.mu.Lock()
	c.gauge.running++
	c.gauge.peak = max(c.gauge.peak, c.gauge.running)
	c.gauge.mu.Unlock()
	time.Sleep(time.Millisecond)
	return nil
}

func (c *swapChannel) Stop(ctx context.Context) error {
	c.gauge.mu.Lock()
	c.gauge.running--
	c.gauge.mu.Unlock()
	return c.fakeDeliverer.Stop(ctx)
}

func registered(reg *Registry, name string) Channel {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	return reg.channels[name]
}

func TestRegistryReplaceStopsStartedInstanceAndStartsNew(t *testing.T) {
	ctx := context.Background()
	old := &fakeDeliverer{name: "telegram", delivered: true}
	next := &fakeDeliverer{name: "telegram", delivered: true}
	reg := NewRegistry()
	reg.Register(old)
	if err := reg.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	if err := reg.Replace(ctx, next); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	if old.stops() != 1 || next.starts() != 1 {
		t.Fatalf("old stops=%d next starts=%d, want 1/1", old.stops(), next.starts())
	}
	if ok, err := reg.DeliverToIdentity(ctx, "id-1", "hi"); err != nil || !ok {
		t.Fatalf("DeliverToIdentity = (%v,%v), want (true,nil)", ok, err)
	}
	if old.delivers() != 0 || next.delivers() != 1 {
		t.Fatalf("delivery reached old=%d next=%d, want 0/1", old.delivers(), next.delivers())
	}
	if err := reg.StopAll(ctx); err != nil {
		t.Fatalf("StopAll: %v", err)
	}
	if old.stops() != 1 || next.stops() != 1 {
		t.Fatalf("after StopAll old stops=%d next stops=%d, want 1/1", old.stops(), next.stops())
	}
}

// The appliance incident: boot had no token, so the boot instance failed to start and
// is registered but not running. Replace must start the new one and leave the dead
// one alone.
func TestRegistryReplaceAfterFailedBootStartOnlyStartsNew(t *testing.T) {
	ctx := context.Background()
	boot := &fakeChannel{name: "telegram", startErr: errors.New("construct bot: Not Found (404)")}
	next := &fakeChannel{name: "telegram"}
	reg := NewRegistry()
	reg.Register(boot)
	if err := reg.StartAll(ctx); err == nil {
		t.Fatal("StartAll: want the boot start failure, got nil")
	}

	if err := reg.Replace(ctx, next); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	if boot.stops() != 0 {
		t.Fatalf("never-started boot instance stops = %d, want 0", boot.stops())
	}
	if next.starts() != 1 {
		t.Fatalf("next starts = %d, want 1", next.starts())
	}
	if err := reg.StopAll(ctx); err != nil {
		t.Fatalf("StopAll: %v", err)
	}
	if next.stops() != 1 {
		t.Fatalf("next stops = %d, want 1 (Replace must record it as started)", next.stops())
	}
}

func TestRegistryReplaceDisabledChannelIsRegisteredButNotStarted(t *testing.T) {
	ctx := context.Background()
	boot := &fakeChannel{name: "telegram"}
	next := &fakeChannel{name: "telegram"}
	reg := NewRegistry()
	reg.Register(boot)
	reg.SetEnabledOverride(func(name string) (bool, bool) { return false, name == "telegram" })
	if err := reg.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	err := reg.Replace(ctx, next)

	if !errors.Is(err, ErrChannelDisabled) {
		t.Fatalf("Replace error = %v, want ErrChannelDisabled", err)
	}
	if next.starts() != 0 {
		t.Fatalf("disabled replacement starts = %d, want 0", next.starts())
	}
	if got := registered(reg, "telegram"); got != next {
		t.Fatalf("registered = %v, want the replacement", got)
	}
	if err := reg.StopAll(ctx); err != nil {
		t.Fatalf("StopAll: %v", err)
	}
	if next.stops() != 0 {
		t.Fatalf("never-started replacement stops = %d, want 0", next.stops())
	}
}

func TestRegistryReplaceStartFailureLeavesItRegisteredNotStarted(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("construct bot: Unauthorized (401)")
	old := &fakeDeliverer{name: "telegram", delivered: true}
	next := &fakeDeliverer{name: "telegram", startErr: boom, delivered: true}
	reg := NewRegistry()
	reg.Register(old)
	if err := reg.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	err := reg.Replace(ctx, next)

	if !errors.Is(err, boom) {
		t.Fatalf("Replace error = %v, want it to wrap %v", err, boom)
	}
	if old.stops() != 1 {
		t.Fatalf("old stops = %d, want 1", old.stops())
	}
	if got := registered(reg, "telegram"); got != next {
		t.Fatalf("registered = %v, want the failed replacement", got)
	}
	if ok, err := reg.DeliverToIdentity(ctx, "id-1", "hi"); ok || err != nil {
		t.Fatalf("DeliverToIdentity = (%v,%v), want (false,nil): nothing runs under the name", ok, err)
	}
	if err := reg.StopAll(ctx); err != nil {
		t.Fatalf("StopAll: %v", err)
	}
	if next.stops() != 0 {
		t.Fatalf("never-started replacement stops = %d, want 0", next.stops())
	}
}

// drainShutdown stops the channels before the HTTP server, so a settings write can
// still land after StopAll. It must not start a poller nobody will ever stop.
func TestRegistryReplaceAfterStopAllRefuses(t *testing.T) {
	ctx := context.Background()
	reg := NewRegistry()
	reg.Register(&fakeChannel{name: "telegram"})
	if err := reg.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	if err := reg.StopAll(ctx); err != nil {
		t.Fatalf("StopAll: %v", err)
	}
	next := &fakeChannel{name: "telegram"}

	if err := reg.Replace(ctx, next); !errors.Is(err, ErrRegistryStopped) {
		t.Fatalf("Replace after StopAll = %v, want ErrRegistryStopped", err)
	}
	if next.starts() != 0 {
		t.Fatalf("next starts = %d, want 0", next.starts())
	}
}

func TestRegistryStopStopsOnlyTheNamedChannel(t *testing.T) {
	ctx := context.Background()
	drainErr := errors.New("drain failed")
	tg := &fakeChannel{name: "telegram", stopErr: drainErr}
	other := &fakeChannel{name: "cli"}
	reg := NewRegistry()
	reg.Register(tg)
	reg.Register(other)
	if err := reg.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	if err := reg.Stop(ctx, "telegram"); !errors.Is(err, drainErr) {
		t.Fatalf("Stop error = %v, want it to wrap %v", err, drainErr)
	}
	if err := reg.Stop(ctx, "telegram"); err != nil {
		t.Fatalf("second Stop = %v, want a no-op", err)
	}
	if tg.stops() != 1 || other.stops() != 0 {
		t.Fatalf("telegram stops=%d cli stops=%d, want 1/0", tg.stops(), other.stops())
	}
	if err := reg.StopAll(ctx); err != nil {
		t.Fatalf("StopAll: %v", err)
	}
	if tg.stops() != 1 || other.stops() != 1 {
		t.Fatalf("after StopAll telegram stops=%d cli stops=%d, want 1/1", tg.stops(), other.stops())
	}
}

func TestRegistryReplaceConcurrentWithDeliveryNeverOverlapsInstances(t *testing.T) {
	ctx := context.Background()
	gauge := &runningGauge{}
	reg := NewRegistry()
	instances := make([]*swapChannel, 8)
	for i := range instances {
		instances[i] = &swapChannel{
			name: "telegram", delivered: true,
			gauge: gauge,
		}
	}

	done := make(chan struct{})
	var deliverers sync.WaitGroup
	for range 4 {
		deliverers.Go(func() {
			for {
				select {
				case <-done:
					return
				default:
				}
				if _, err := reg.DeliverToIdentity(ctx, "id-1", "hi"); err != nil {
					t.Errorf("DeliverToIdentity: %v", err)
					return
				}
				if _, err := reg.DeliverToConversation(ctx, "id-1", "conv-1", "hi"); err != nil {
					t.Errorf("DeliverToConversation: %v", err)
					return
				}
				if _, err := reg.DeliverApproval(ctx, "id-1", "tok", "task", "agent_job"); err != nil {
					t.Errorf("DeliverApproval: %v", err)
					return
				}
			}
		})
	}
	var replacers sync.WaitGroup
	for _, c := range instances {
		replacers.Go(func() {
			if err := reg.Replace(ctx, c); err != nil {
				t.Errorf("Replace: %v", err)
			}
		})
	}
	replacers.Wait()
	close(done)
	deliverers.Wait()

	gauge.mu.Lock()
	running, peak := gauge.running, gauge.peak
	gauge.mu.Unlock()
	if peak != 1 || running != 1 {
		t.Fatalf("instances running=%d peak=%d, want 1/1 (Replace calls interleaved)", running, peak)
	}
	if err := reg.StopAll(ctx); err != nil {
		t.Fatalf("StopAll: %v", err)
	}
	if gauge.running != 0 {
		t.Fatalf("running after StopAll = %d, want 0", gauge.running)
	}
}
