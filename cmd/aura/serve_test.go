package main

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/channels"
	"github.com/chetto1983/aura/internal/channels/telegram"
	"github.com/chetto1983/aura/internal/config"
)

// fakeChannel is a channels.Channel double for the serve lifecycle tests. It
// records Start/Stop invocations and can be forced to fail Start (fail-soft
// assertion). No network, no DB — exercises ONLY the registry mount lifecycle.
type fakeChannel struct {
	name     string
	startErr error
	started  atomic.Int32
	stopped  atomic.Int32
	healthy  atomic.Bool
}

func (f *fakeChannel) Name() string { return f.name }

func (f *fakeChannel) Start(_ context.Context) error {
	if f.startErr != nil {
		return f.startErr
	}
	f.started.Add(1)
	f.healthy.Store(true)
	return nil
}

func (f *fakeChannel) Stop(_ context.Context) error {
	f.stopped.Add(1)
	f.healthy.Store(false)
	return nil
}

func (f *fakeChannel) IsHealthy() bool { return f.healthy.Load() }

// loopbackSetupSrv builds a real *http.Server bound to an ephemeral loopback
// port so startChannelSubsystems can ListenAndServe it and stopChannelSubsystems
// can Shutdown it (the production setup-server lifecycle, no live bot).
func loopbackSetupSrv() *http.Server {
	return &http.Server{
		Addr:    "127.0.0.1:0",
		Handler: http.NewServeMux(),
	}
}

// TestStartStopChannelSubsystemsLifecycle proves the serve-channels lifecycle
// helpers (the Task-1 mount): StartAll starts an enabled channel, stop joins it,
// and the setup HTTP server is launched + shut down — all goleak-clean (the
// package TestMain catches a leaked poller/listener).
func TestStartStopChannelSubsystemsLifecycle(t *testing.T) {
	reg := channels.NewRegistry()
	fc := &fakeChannel{name: "fake"}
	reg.Register(fc)
	setupSrv := loopbackSetupSrv()

	ctx := context.Background()
	startChannelSubsystems(ctx, reg, setupSrv)
	// Give the setup-server goroutine a moment to bind.
	time.Sleep(20 * time.Millisecond)

	if fc.started.Load() != 1 {
		t.Fatalf("StartAll did not start the enabled channel: started=%d", fc.started.Load())
	}
	if !fc.IsHealthy() {
		t.Fatal("channel must be healthy after StartAll")
	}

	stopChannelSubsystems(ctx, reg, setupSrv)
	if fc.stopped.Load() != 1 {
		t.Fatalf("StopAll did not stop the started channel: stopped=%d", fc.stopped.Load())
	}
	if fc.IsHealthy() {
		t.Fatal("channel must be unhealthy after StopAll")
	}
}

// TestStartChannelSubsystemsFailSoft proves a failing channel Start does NOT
// abort the daemon (fail-soft, T-13-09-DaemonAbort): the registry aggregates the
// error and the healthy sibling still starts. startChannelSubsystems must never
// panic or propagate a fatal — the daemon keeps running.
func TestStartChannelSubsystemsFailSoft(t *testing.T) {
	reg := channels.NewRegistry()
	bad := &fakeChannel{name: "bad", startErr: errors.New("boom: bot construct failed")}
	good := &fakeChannel{name: "good"}
	reg.Register(bad)
	reg.Register(good)
	setupSrv := loopbackSetupSrv()

	ctx := context.Background()
	// Must not panic / must not block forever: a failed channel is logged + skipped.
	startChannelSubsystems(ctx, reg, setupSrv)
	time.Sleep(20 * time.Millisecond)
	defer stopChannelSubsystems(ctx, reg, setupSrv)

	if good.started.Load() != 1 {
		t.Fatalf("a failing sibling must not abort the healthy channel: good.started=%d", good.started.Load())
	}
	if bad.started.Load() != 0 {
		t.Fatalf("the failing channel must not record a start: bad.started=%d", bad.started.Load())
	}
	if bad.IsHealthy() {
		t.Fatal("the failing channel must not be healthy")
	}
}

// TestStopChannelSubsystemsIdempotent proves stop is safe to call with no started
// channels and is a clean no-op (serve's defer must never double-fault).
func TestStopChannelSubsystemsIdempotent(t *testing.T) {
	reg := channels.NewRegistry()
	fc := &fakeChannel{name: "fake"}
	reg.Register(fc)
	setupSrv := loopbackSetupSrv()
	ctx := context.Background()

	// Stop before any Start: a clean no-op (nothing started → nothing stopped).
	stopChannelSubsystems(ctx, reg, setupSrv)
	if fc.stopped.Load() != 0 {
		t.Fatalf("stop before start must stop nothing, got stopped=%d", fc.stopped.Load())
	}
}

// TestServeFlagsDisableTelegram proves --no-telegram / --only=cli install a
// registry override that disables the telegram channel (PRD Punto 1), while a
// bare serve leaves the env enable gate intact (no override).
func TestServeFlagsDisableTelegram(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantDisabled bool
	}{
		{"bare serve — no override", nil, false},
		{"--no-telegram disables", []string{"--no-telegram"}, true},
		{"--only=cli disables", []string{"--only=cli"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			override, ok := serveTelegramOverride(tc.args)
			if tc.wantDisabled {
				if !ok {
					t.Fatalf("%q must install an override", tc.args)
				}
				on, present := override("telegram")
				if !present || on {
					t.Fatalf("%q must disable telegram: on=%v present=%v", tc.args, on, present)
				}
			} else if ok {
				t.Fatalf("bare serve must install NO override, got one")
			}
		})
	}
}

func TestBuildTelegramDepsUsesSharedAssetService(t *testing.T) {
	assetSvc := &assets.Service{}
	chat := &chatEnv{
		cfg:    &config.Config{ProfileDir: t.TempDir()},
		assets: assetSvc,
	}

	deps := buildTelegramDeps(chat, telegram.Config{})

	if deps.Assets != assetSvc {
		t.Fatalf("telegram deps assets = %T, want shared asset service", deps.Assets)
	}
}
