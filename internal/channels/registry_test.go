package channels

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// fakeChannel is the test double standing in for a real daemon channel. It
// records Start/Stop calls and can be configured to fail either, so the
// registry's fail-soft + enable-gate + errors.Join contract is exercised without
// a live bot.
type fakeChannel struct {
	name     string
	startErr error
	stopErr  error

	mu         sync.Mutex
	startCalls int
	stopCalls  int
	healthy    bool
}

func (c *fakeChannel) Name() string { return c.name }

func (c *fakeChannel) Start(context.Context) error {
	c.mu.Lock()
	c.startCalls++
	c.mu.Unlock()
	return c.startErr
}

func (c *fakeChannel) Stop(context.Context) error {
	c.mu.Lock()
	c.stopCalls++
	c.mu.Unlock()
	return c.stopErr
}

func (c *fakeChannel) IsHealthy() bool { return c.healthy }

func (c *fakeChannel) starts() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.startCalls
}

func (c *fakeChannel) stops() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stopCalls
}

func TestRegistryStartAllStartsEnabledChannels(t *testing.T) {
	a := &fakeChannel{name: "alpha"}
	b := &fakeChannel{name: "beta"}
	reg := NewRegistry()
	reg.Register(a)
	reg.Register(b)

	if err := reg.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: unexpected error: %v", err)
	}
	if a.starts() != 1 {
		t.Errorf("alpha starts = %d, want 1", a.starts())
	}
	if b.starts() != 1 {
		t.Errorf("beta starts = %d, want 1", b.starts())
	}
}

func TestRegistryStartAllFailSoftAggregatesErrors(t *testing.T) {
	failErr := errors.New("boom")
	failing := &fakeChannel{name: "failing", startErr: failErr}
	healthy := &fakeChannel{name: "healthy"}
	reg := NewRegistry()
	reg.Register(failing)
	reg.Register(healthy)

	err := reg.StartAll(context.Background())
	if err == nil {
		t.Fatal("StartAll: want aggregated error, got nil")
	}
	if !errors.Is(err, failErr) {
		t.Errorf("StartAll error = %v, want it to wrap %v", err, failErr)
	}
	// Fail-soft: the failing channel must NOT abort the sibling.
	if healthy.starts() != 1 {
		t.Errorf("healthy starts = %d, want 1 (one failure must not abort siblings)", healthy.starts())
	}
}

func TestRegistryEnableGateSkipsDisabledChannel(t *testing.T) {
	disabled := &fakeChannel{name: "disabled"}
	enabled := &fakeChannel{name: "enabled"}
	reg := NewRegistry()
	reg.Register(disabled)
	reg.Register(enabled)

	t.Setenv("AURA_CHANNEL_DISABLED_ENABLED", "false")

	if err := reg.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: unexpected error: %v", err)
	}
	if disabled.starts() != 0 {
		t.Errorf("disabled starts = %d, want 0 (enable gate respected)", disabled.starts())
	}
	if enabled.starts() != 1 {
		t.Errorf("enabled starts = %d, want 1", enabled.starts())
	}
}

func TestRegistryDefaultEnabledTrue(t *testing.T) {
	c := &fakeChannel{name: "gamma"}
	reg := NewRegistry()
	reg.Register(c)

	// No AURA_CHANNEL_GAMMA_ENABLED set → defaults to true.
	if err := reg.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: unexpected error: %v", err)
	}
	if c.starts() != 1 {
		t.Errorf("gamma starts = %d, want 1 (default-enabled)", c.starts())
	}
}

func TestRegistryStopAllOnlyStopsStarted(t *testing.T) {
	started := &fakeChannel{name: "started"}
	disabled := &fakeChannel{name: "skip"}
	reg := NewRegistry()
	reg.Register(started)
	reg.Register(disabled)

	t.Setenv("AURA_CHANNEL_SKIP_ENABLED", "false")

	if err := reg.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: unexpected error: %v", err)
	}
	if err := reg.StopAll(context.Background()); err != nil {
		t.Fatalf("StopAll: unexpected error: %v", err)
	}
	if started.stops() != 1 {
		t.Errorf("started stops = %d, want 1", started.stops())
	}
	// A disabled (never-started) channel must NOT be stopped.
	if disabled.stops() != 0 {
		t.Errorf("skip stops = %d, want 0 (StopAll only stops what it started)", disabled.stops())
	}
}

func TestRegistryStopAllAggregatesErrors(t *testing.T) {
	stopErr := errors.New("drain failed")
	a := &fakeChannel{name: "a", stopErr: stopErr}
	b := &fakeChannel{name: "b"}
	reg := NewRegistry()
	reg.Register(a)
	reg.Register(b)

	if err := reg.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: unexpected error: %v", err)
	}
	err := reg.StopAll(context.Background())
	if err == nil {
		t.Fatal("StopAll: want aggregated error, got nil")
	}
	if !errors.Is(err, stopErr) {
		t.Errorf("StopAll error = %v, want it to wrap %v", err, stopErr)
	}
	if b.stops() != 1 {
		t.Errorf("b stops = %d, want 1 (one Stop failure must not abort siblings)", b.stops())
	}
}

func TestRegistryStopAllIdempotent(t *testing.T) {
	c := &fakeChannel{name: "c"}
	reg := NewRegistry()
	reg.Register(c)

	if err := reg.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: unexpected error: %v", err)
	}
	if err := reg.StopAll(context.Background()); err != nil {
		t.Fatalf("StopAll #1: unexpected error: %v", err)
	}
	if err := reg.StopAll(context.Background()); err != nil {
		t.Fatalf("StopAll #2: unexpected error: %v", err)
	}
	// Second StopAll is a no-op — the channel was already stopped.
	if c.stops() != 1 {
		t.Errorf("c stops = %d, want 1 (StopAll idempotent)", c.stops())
	}
}

func TestRegistryStopAllBeforeStartAllNoop(t *testing.T) {
	c := &fakeChannel{name: "c"}
	reg := NewRegistry()
	reg.Register(c)

	// StopAll with nothing started is a clean no-op.
	if err := reg.StopAll(context.Background()); err != nil {
		t.Fatalf("StopAll: unexpected error: %v", err)
	}
	if c.stops() != 0 {
		t.Errorf("c stops = %d, want 0 (nothing started)", c.stops())
	}
}

func TestRegistryOverrideForcesChannelOff(t *testing.T) {
	// The serve.go --no-telegram / --only=cli flags (parsed in plan 13-09)
	// override the env gate. Here the registry accepts an override predicate.
	off := &fakeChannel{name: "telegram"}
	on := &fakeChannel{name: "cli"}
	reg := NewRegistry()
	reg.Register(off)
	reg.Register(on)

	// Env says enabled (default), but the override forces telegram off.
	reg.SetEnabledOverride(func(name string) (bool, bool) {
		if name == "telegram" {
			return false, true // forced off
		}
		return false, false // no override → fall back to env
	})

	if err := reg.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: unexpected error: %v", err)
	}
	if off.starts() != 0 {
		t.Errorf("telegram starts = %d, want 0 (override forced off)", off.starts())
	}
	if on.starts() != 1 {
		t.Errorf("cli starts = %d, want 1", on.starts())
	}
}

func TestRegistryOverrideForcesChannelOnDespiteEnvDisabled(t *testing.T) {
	c := &fakeChannel{name: "telegram"}
	reg := NewRegistry()
	reg.Register(c)

	// Env disables, but the override forces it on (e.g. --only=telegram).
	t.Setenv("AURA_CHANNEL_TELEGRAM_ENABLED", "false")
	reg.SetEnabledOverride(func(name string) (bool, bool) {
		if name == "telegram" {
			return true, true // forced on
		}
		return false, false
	})

	if err := reg.StartAll(context.Background()); err != nil {
		t.Fatalf("StartAll: unexpected error: %v", err)
	}
	if c.starts() != 1 {
		t.Errorf("telegram starts = %d, want 1 (override forced on despite env=false)", c.starts())
	}
}

// fakeDeliverer is a fakeChannel that ALSO implements Deliverer with a configurable
// tri-state return + a mutex-guarded call counter. It lets TestRegistryDeliverToIdentity
// exercise the fan-out contract (sorted order, first-delivers-wins, owns-but-fails
// stops, not-started never asked) with no live channel.
type fakeDeliverer struct {
	fakeChannel
	delivered    bool
	deliverErr   error
	mu           sync.Mutex
	deliverCalls int
}

func (d *fakeDeliverer) Deliver(_ context.Context, _, _ string) (bool, error) {
	d.mu.Lock()
	d.deliverCalls++
	d.mu.Unlock()
	return d.delivered, d.deliverErr
}

func (d *fakeDeliverer) delivers() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.deliverCalls
}

type fakeConversationDeliverer struct {
	fakeChannel
	delivered        bool
	deliverErr       error
	mu               sync.Mutex
	deliverCalls     int
	lastIdentity     string
	lastConversation string
}

func (d *fakeConversationDeliverer) DeliverConversation(_ context.Context, identityID, conversationID, _ string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deliverCalls++
	d.lastIdentity = identityID
	d.lastConversation = conversationID
	return d.delivered, d.deliverErr
}

func (d *fakeConversationDeliverer) delivers() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.deliverCalls
}

// fakeApprovalDeliverer is a fakeChannel that ALSO implements ApprovalDeliverer, mirroring
// fakeDeliverer, so TestRegistryDeliverApproval exercises the actionable-approval fan-out.
type fakeApprovalDeliverer struct {
	fakeChannel
	delivered  bool
	deliverErr error
	mu         sync.Mutex
	calls      int
	lastToken  string
}

func (d *fakeApprovalDeliverer) DeliverApproval(_ context.Context, _, token, _, _ string) (bool, error) {
	d.mu.Lock()
	d.calls++
	d.lastToken = token
	d.mu.Unlock()
	return d.delivered, d.deliverErr
}

func (d *fakeApprovalDeliverer) delivers() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

func TestRegistryDeliverApproval(t *testing.T) {
	errSend := errors.New("send failed")

	t.Run("first-delivers-wins in sorted order carries the token", func(t *testing.T) {
		a := &fakeApprovalDeliverer{fakeChannel: fakeChannel{name: "a"}, delivered: true}
		b := &fakeApprovalDeliverer{fakeChannel: fakeChannel{name: "b"}, delivered: true}
		reg := NewRegistry()
		reg.Register(b)
		reg.Register(a)
		if err := reg.StartAll(context.Background()); err != nil {
			t.Fatalf("StartAll: %v", err)
		}
		ok, err := reg.DeliverApproval(context.Background(), "id-1", "tok-9", "task-1", "agent_job")
		if err != nil || !ok {
			t.Fatalf("DeliverApproval = (%v,%v), want (true,nil)", ok, err)
		}
		if a.delivers() != 1 || a.lastToken != "tok-9" {
			t.Errorf("a calls=%d token=%q, want 1/tok-9", a.delivers(), a.lastToken)
		}
		if b.delivers() != 0 {
			t.Errorf("b must not be asked once a delivered, got %d", b.delivers())
		}
	})

	t.Run("no owner returns (false,nil) so the caller falls back to the pull surface", func(t *testing.T) {
		a := &fakeApprovalDeliverer{fakeChannel: fakeChannel{name: "a"}} // delivered=false
		reg := NewRegistry()
		reg.Register(a)
		if err := reg.StartAll(context.Background()); err != nil {
			t.Fatalf("StartAll: %v", err)
		}
		ok, err := reg.DeliverApproval(context.Background(), "webui-id", "tok", "task", "agent_job")
		if ok || err != nil {
			t.Fatalf("DeliverApproval = (%v,%v), want (false,nil)", ok, err)
		}
	})

	t.Run("owns-but-fails stops the fan-out (no sibling double-delivery)", func(t *testing.T) {
		a := &fakeApprovalDeliverer{fakeChannel: fakeChannel{name: "a"}, deliverErr: errSend}
		b := &fakeApprovalDeliverer{fakeChannel: fakeChannel{name: "b"}, delivered: true}
		reg := NewRegistry()
		reg.Register(a)
		reg.Register(b)
		if err := reg.StartAll(context.Background()); err != nil {
			t.Fatalf("StartAll: %v", err)
		}
		ok, err := reg.DeliverApproval(context.Background(), "id-1", "tok", "task", "agent_job")
		if ok || !errors.Is(err, errSend) {
			t.Fatalf("DeliverApproval = (%v,%v), want (false, errSend)", ok, err)
		}
		if b.delivers() != 0 {
			t.Errorf("b must NOT be asked after a owns-but-fails, got %d", b.delivers())
		}
	})

	t.Run("channel without ApprovalDeliverer is skipped", func(t *testing.T) {
		plain := &fakeChannel{name: "a-plain"} // Channel but NOT ApprovalDeliverer
		appr := &fakeApprovalDeliverer{fakeChannel: fakeChannel{name: "b-appr"}, delivered: true}
		reg := NewRegistry()
		reg.Register(plain)
		reg.Register(appr)
		if err := reg.StartAll(context.Background()); err != nil {
			t.Fatalf("StartAll: %v", err)
		}
		ok, err := reg.DeliverApproval(context.Background(), "id-1", "tok", "task", "agent_job")
		if err != nil || !ok {
			t.Fatalf("DeliverApproval = (%v,%v), want (true,nil) (plain skipped, approver answers)", ok, err)
		}
	})
}

func TestRegistryDeliverToIdentity(t *testing.T) {
	errSend := errors.New("send failed")

	t.Run("first-delivers-wins in sorted order", func(t *testing.T) {
		// Insertion order (b, a) differs from sort order (a, b): "a" is tried first
		// and delivers, so "b" must never be asked. Proves SORT order, not insertion.
		a := &fakeDeliverer{fakeChannel: fakeChannel{name: "a"}, delivered: true}
		b := &fakeDeliverer{fakeChannel: fakeChannel{name: "b"}, delivered: true}
		reg := NewRegistry()
		reg.Register(b)
		reg.Register(a)
		if err := reg.StartAll(context.Background()); err != nil {
			t.Fatalf("StartAll: %v", err)
		}

		ok, err := reg.DeliverToIdentity(context.Background(), "id-1", "hi")
		if err != nil {
			t.Fatalf("DeliverToIdentity: unexpected error: %v", err)
		}
		if !ok {
			t.Error("want delivered=true (a owns the identity)")
		}
		if a.delivers() != 1 {
			t.Errorf("a delivers = %d, want 1 (sorted-first tried)", a.delivers())
		}
		if b.delivers() != 0 {
			t.Errorf("b delivers = %d, want 0 (first-delivers-wins stops fan-out)", b.delivers())
		}
	})

	t.Run("not-my-user fall-through returns false,nil", func(t *testing.T) {
		a := &fakeDeliverer{fakeChannel: fakeChannel{name: "a"}}
		b := &fakeDeliverer{fakeChannel: fakeChannel{name: "b"}}
		reg := NewRegistry()
		reg.Register(a)
		reg.Register(b)
		if err := reg.StartAll(context.Background()); err != nil {
			t.Fatalf("StartAll: %v", err)
		}

		ok, err := reg.DeliverToIdentity(context.Background(), "id-1", "hi")
		if err != nil {
			t.Fatalf("DeliverToIdentity: unexpected error: %v", err)
		}
		if ok {
			t.Error("want delivered=false (no channel owns the identity)")
		}
		if a.delivers() != 1 || b.delivers() != 1 {
			t.Errorf("both must be asked: a=%d b=%d, want 1 1", a.delivers(), b.delivers())
		}
	})

	t.Run("owns-but-fails stops without sibling attempt", func(t *testing.T) {
		// Sorted-first "a" owns-but-fails → fan-out stops; the sibling "b" is never asked.
		a := &fakeDeliverer{fakeChannel: fakeChannel{name: "a"}, deliverErr: errSend}
		b := &fakeDeliverer{fakeChannel: fakeChannel{name: "b"}, delivered: true}
		reg := NewRegistry()
		reg.Register(a)
		reg.Register(b)
		if err := reg.StartAll(context.Background()); err != nil {
			t.Fatalf("StartAll: %v", err)
		}

		ok, err := reg.DeliverToIdentity(context.Background(), "id-1", "hi")
		if !errors.Is(err, errSend) {
			t.Fatalf("DeliverToIdentity error = %v, want it to wrap %v", err, errSend)
		}
		if ok {
			t.Error("want delivered=false on owns-but-fails")
		}
		if b.delivers() != 0 {
			t.Errorf("b delivers = %d, want 0 (owns-but-fails must not try siblings)", b.delivers())
		}
	})

	t.Run("not-started channel never asked", func(t *testing.T) {
		// "a" is registered + started; "b" is registered but disabled (never started).
		a := &fakeDeliverer{fakeChannel: fakeChannel{name: "a"}}
		b := &fakeDeliverer{fakeChannel: fakeChannel{name: "b"}}
		reg := NewRegistry()
		reg.Register(a)
		reg.Register(b)
		t.Setenv("AURA_CHANNEL_B_ENABLED", "false")
		if err := reg.StartAll(context.Background()); err != nil {
			t.Fatalf("StartAll: %v", err)
		}

		if _, err := reg.DeliverToIdentity(context.Background(), "id-1", "hi"); err != nil {
			t.Fatalf("DeliverToIdentity: unexpected error: %v", err)
		}
		if b.delivers() != 0 {
			t.Errorf("b delivers = %d, want 0 (not-started never asked)", b.delivers())
		}
	})

	t.Run("started channel without Deliverer silently skipped", func(t *testing.T) {
		// A plain fakeChannel (Channel but NOT Deliverer) is started alongside a
		// fakeDeliverer: the non-Deliverer is skipped (no panic), the Deliverer answers.
		plain := &fakeChannel{name: "a-plain"}
		deliv := &fakeDeliverer{fakeChannel: fakeChannel{name: "b-deliv"}, delivered: true}
		reg := NewRegistry()
		reg.Register(plain)
		reg.Register(deliv)
		if err := reg.StartAll(context.Background()); err != nil {
			t.Fatalf("StartAll: %v", err)
		}

		ok, err := reg.DeliverToIdentity(context.Background(), "id-1", "hi")
		if err != nil {
			t.Fatalf("DeliverToIdentity: unexpected error: %v", err)
		}
		if !ok {
			t.Error("want delivered=true (the Deliverer owns it; the non-Deliverer is skipped)")
		}
		if deliv.delivers() != 1 {
			t.Errorf("deliverer delivers = %d, want 1", deliv.delivers())
		}
	})
}

func TestRegistryDeliverToConversation(t *testing.T) {
	errSend := errors.New("send failed")

	t.Run("passes exact identity and conversation in sorted order", func(t *testing.T) {
		a := &fakeConversationDeliverer{fakeChannel: fakeChannel{name: "a"}, delivered: true}
		b := &fakeConversationDeliverer{fakeChannel: fakeChannel{name: "b"}, delivered: true}
		reg := NewRegistry()
		reg.Register(b)
		reg.Register(a)
		if err := reg.StartAll(context.Background()); err != nil {
			t.Fatalf("StartAll: %v", err)
		}
		ok, err := reg.DeliverToConversation(context.Background(), "id-1", "conv-1", "hi")
		if err != nil || !ok {
			t.Fatalf("DeliverToConversation = (%v, %v), want (true, nil)", ok, err)
		}
		if a.lastIdentity != "id-1" || a.lastConversation != "conv-1" {
			t.Fatalf("route = (%q, %q), want (id-1, conv-1)", a.lastIdentity, a.lastConversation)
		}
		if b.delivers() != 0 {
			t.Fatalf("second channel calls = %d, want 0", b.delivers())
		}
	})

	t.Run("never falls back to identity-only deliverer", func(t *testing.T) {
		identityOnly := &fakeDeliverer{fakeChannel: fakeChannel{name: "identity"}, delivered: true}
		reg := NewRegistry()
		reg.Register(identityOnly)
		if err := reg.StartAll(context.Background()); err != nil {
			t.Fatalf("StartAll: %v", err)
		}
		ok, err := reg.DeliverToConversation(context.Background(), "id-1", "cockpit-conv", "hi")
		if err != nil || ok {
			t.Fatalf("DeliverToConversation = (%v, %v), want (false, nil)", ok, err)
		}
		if identityOnly.delivers() != 0 {
			t.Fatalf("identity-only calls = %d, want 0", identityOnly.delivers())
		}
	})

	t.Run("owns-but-failed stops siblings", func(t *testing.T) {
		a := &fakeConversationDeliverer{fakeChannel: fakeChannel{name: "a"}, deliverErr: errSend}
		b := &fakeConversationDeliverer{fakeChannel: fakeChannel{name: "b"}, delivered: true}
		reg := NewRegistry()
		reg.Register(a)
		reg.Register(b)
		if err := reg.StartAll(context.Background()); err != nil {
			t.Fatalf("StartAll: %v", err)
		}
		ok, err := reg.DeliverToConversation(context.Background(), "id-1", "conv-1", "hi")
		if ok || !errors.Is(err, errSend) {
			t.Fatalf("DeliverToConversation = (%v, %v), want (false, errSend)", ok, err)
		}
		if b.delivers() != 0 {
			t.Fatalf("sibling calls = %d, want 0", b.delivers())
		}
	})

	t.Run("empty route fails closed", func(t *testing.T) {
		d := &fakeConversationDeliverer{fakeChannel: fakeChannel{name: "a"}, delivered: true}
		reg := NewRegistry()
		reg.Register(d)
		if err := reg.StartAll(context.Background()); err != nil {
			t.Fatalf("StartAll: %v", err)
		}
		for _, tc := range []struct{ identity, conversation string }{{"", "conv"}, {"id", ""}} {
			ok, err := reg.DeliverToConversation(context.Background(), tc.identity, tc.conversation, "hi")
			if ok || err != nil {
				t.Fatalf("DeliverToConversation(%q,%q) = (%v,%v), want (false,nil)", tc.identity, tc.conversation, ok, err)
			}
		}
		if d.delivers() != 0 {
			t.Fatalf("calls = %d, want 0", d.delivers())
		}
	})
}
