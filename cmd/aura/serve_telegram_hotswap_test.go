package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/channels"
)

// swapFake is a Telegram channel with no bot behind it: healthy exactly while started,
// like the real one, and failing Start with whatever its fixture assigned its token. It
// keeps the ctx Start received, the one the real channel parents every turn on.
type swapFake struct {
	startErr error

	mu       sync.Mutex
	running  bool
	stopped  int
	startedN int
	startCtx context.Context
}

func (f *swapFake) Name() string { return "telegram" }

func (f *swapFake) Start(ctx context.Context) error {
	if f.startErr != nil {
		return f.startErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.running = true
	f.startedN++
	f.startCtx = ctx
	return nil
}

func (f *swapFake) Stop(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.running = false
	f.stopped++
	return nil
}

func (f *swapFake) IsHealthy() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running
}

type swapFixture struct {
	reg      *channels.Registry
	swap     *telegramHotSwap
	built    map[string]*swapFake
	startErr map[string]error
}

func newSwapFixture(daemon context.Context, bootToken string, startErr map[string]error) *swapFixture {
	f := &swapFixture{reg: channels.NewRegistry(), built: map[string]*swapFake{}, startErr: startErr}
	f.swap = newTelegramHotSwap(daemon, f.reg, bootToken, func(token string) channels.Channel {
		c := &swapFake{startErr: f.startErr[token]}
		f.built[token] = c
		return c
	})
	return f
}

// The appliance incident: boot had no token, so the boot channel could not start.
func TestTelegramHotSwapStartsSavedTokenOnFreshAppliance(t *testing.T) {
	ctx := context.Background()
	const token = "123456:fresh-appliance"
	f := newSwapFixture(ctx, "", map[string]error{"": errors.New("construct bot: Not Found (404)")})
	if err := f.reg.StartAll(ctx); err == nil {
		t.Fatal("StartAll: want the empty-token boot failure, got nil")
	}

	if err := f.swap.Activate(ctx, token); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	if !f.swap.Runs(token) {
		t.Fatal("Runs(saved token) = false after a successful activation")
	}
	if f.swap.Runs("999:other") || f.swap.Runs("") {
		t.Fatal("Runs reported a token the channel does not poll")
	}
	if err := f.reg.StopAll(ctx); err != nil {
		t.Fatalf("StopAll: %v", err)
	}
	if f.built[token].stopped != 1 {
		t.Fatal("the hot-started channel was not recorded as started, so shutdown never stopped it")
	}
}

// Telegram parents every turn on the ctx its Start received. A swap triggered by a
// settings request must start under the daemon's ctx: the request's ends with its
// response, and every turn of the new bot would inherit an already-cancelled ctx.
func TestTelegramHotSwapStartsUnderTheDaemonContextNotTheRequest(t *testing.T) {
	daemon, stopDaemon := context.WithCancel(context.Background())
	defer stopDaemon()
	const token = "123456:ctx-lifetime"
	f := newSwapFixture(daemon, "", map[string]error{"": errors.New("no token")})
	request, endRequest := context.WithCancel(context.Background())

	if err := f.swap.Activate(request, token); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	endRequest()

	started := f.built[token].startCtx
	if started == nil || started.Err() != nil {
		t.Fatal("the new channel's turn context died with the settings request")
	}
	stopDaemon()
	if started.Err() == nil {
		t.Fatal("the new channel's turn context outlives the daemon")
	}
}

func TestTelegramHotSwapReplacesTheRunningToken(t *testing.T) {
	ctx := context.Background()
	const oldToken, newToken = "111:boot-token", "222:saved-token"
	f := newSwapFixture(ctx, oldToken, nil)
	if err := f.reg.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	if !f.swap.Runs(oldToken) {
		t.Fatal("Runs(boot token) = false while the boot channel runs it")
	}

	if err := f.swap.Activate(ctx, newToken); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	if f.built[oldToken].stopped != 1 || f.built[newToken].startedN != 1 {
		t.Fatalf("old stops=%d new starts=%d, want 1/1", f.built[oldToken].stopped, f.built[newToken].startedN)
	}
	if f.swap.Runs(oldToken) || !f.swap.Runs(newToken) {
		t.Fatal("Runs still reports the retired token, or not the new one")
	}
}

func TestTelegramHotSwapEmptyTokenStopsTheChannel(t *testing.T) {
	ctx := context.Background()
	const bootToken = "111:boot-token"
	f := newSwapFixture(ctx, bootToken, nil)
	if err := f.reg.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	if err := f.swap.Activate(ctx, ""); err != nil {
		t.Fatalf("Activate(\"\"): %v", err)
	}

	if f.built[bootToken].stopped != 1 || f.swap.Runs(bootToken) {
		t.Fatal("clearing the token left the channel running")
	}
	if len(f.built) != 1 {
		t.Fatalf("clearing the token built a channel: %v", f.built)
	}
}

func TestTelegramHotSwapReportsSecretFreeReasons(t *testing.T) {
	const token = "123456:reason-secret"
	cases := []struct {
		name     string
		prepare  func(ctx context.Context, reg *channels.Registry)
		startErr error
		want     string
	}{
		{
			name: "disabled by --no-telegram",
			prepare: func(_ context.Context, reg *channels.Registry) {
				reg.SetEnabledOverride(func(name string) (bool, bool) { return false, name == "telegram" })
			},
			want: "telegram channel is disabled on this daemon",
		},
		{
			name:     "start failed",
			startErr: errors.New(`Post "https://api.telegram.org/bot` + token + `/getMe": i/o timeout`),
			want:     "telegram channel failed to start",
		},
		{
			name:    "daemon shutting down",
			prepare: func(ctx context.Context, reg *channels.Registry) { _ = reg.StopAll(ctx) },
			want:    "aura is shutting down",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			f := newSwapFixture(ctx, "", map[string]error{"": errors.New("no token"), token: tc.startErr})
			if tc.prepare != nil {
				tc.prepare(ctx, f.reg)
			}

			err := f.swap.Activate(ctx, token)

			if err == nil || err.Error() != tc.want {
				t.Fatalf("Activate error = %v, want %q", err, tc.want)
			}
			if strings.Contains(err.Error(), token) {
				t.Fatalf("operator-facing reason carries the token: %q", err)
			}
			if f.swap.Runs(token) {
				t.Fatal("Runs = true for a token that never started")
			}
		})
	}
}
