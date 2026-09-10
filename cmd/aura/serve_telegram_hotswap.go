package main

// serve_telegram_hotswap.go puts a Telegram token saved from the web console to work
// without a restart. Measured on an appliance (2026-09-10): the channel was built once
// at boot from TELEGRAM_BOT_TOKEN, a fresh install had none, the wizard saved one, and
// the running daemon never picked it up, while the operator of a box with no monitor
// and no SSH was told to restart it.

import (
	"context"
	"errors"
	"sync"

	"github.com/chetto1983/aura/internal/channels"
)

// The Settings API shows these verbatim as channel_error, so they name the condition
// and never carry the token; the Registry has already logged the underlying cause.
var (
	errTelegramDisabled     = errors.New("telegram channel is disabled on this daemon")
	errTelegramStartFailed  = errors.New("telegram channel failed to start")
	errTelegramStopFailed   = errors.New("telegram channel failed to stop")
	errTelegramShuttingDown = errors.New("aura is shutting down")
)

// telegramHotSwap owns the daemon's Telegram channel instance: it builds the boot one
// and, on every token write, a fresh one over the same deps for Registry.Replace.
type telegramHotSwap struct {
	// daemon is the context every instance starts under, the one StartAll receives.
	// Telegram parents each turn on the ctx its Start got, so a swap must never pass
	// the settings request's ctx, which is cancelled once the response is written.
	daemon context.Context
	reg    *channels.Registry
	build  func(token string) channels.Channel
	name   string

	// mu is held across the whole swap, Replace included, so token and current always
	// describe the instance the Registry swapped in last.
	mu      sync.Mutex
	token   string
	current channels.Channel
}

// newTelegramHotSwap registers the boot instance built from bootToken; StartAll starts
// it together with the other channels.
func newTelegramHotSwap(daemon context.Context, reg *channels.Registry, bootToken string, build func(token string) channels.Channel) *telegramHotSwap {
	boot := build(bootToken)
	reg.Register(boot)
	return &telegramHotSwap{daemon: daemon, reg: reg, build: build, name: boot.Name(), token: bootToken, current: boot}
}

// Activate swaps the running channel onto token, or stops it when token is empty. The
// request ctx is deliberately unused (see daemon). The Registry's enable gate still
// applies: under --no-telegram the new instance is registered but stays off.
func (h *telegramHotSwap) Activate(_ context.Context, token string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if token == "" {
		if err := h.reg.Stop(h.daemon, h.name); err != nil {
			return errTelegramStopFailed
		}
		return nil
	}
	h.token, h.current = token, h.build(token)
	err := h.reg.Replace(h.daemon, h.current)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, channels.ErrChannelDisabled):
		return errTelegramDisabled
	case errors.Is(err, channels.ErrRegistryStopped):
		return errTelegramShuttingDown
	default:
		return errTelegramStartFailed
	}
}

// Runs reports whether the live channel polls with token.
func (h *telegramHotSwap) Runs(token string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return token != "" && token == h.token && h.current.IsHealthy()
}
