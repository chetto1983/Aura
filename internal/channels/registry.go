package channels

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
)

// Registry holds the set of daemon channels and aggregates their lifecycle. It
// mirrors the map-backed internal/agent/tools.Registry idiom (NewRegistry /
// Register) and the fail-soft daemon-subsystem posture of bootServe: one
// channel's Start failure is logged and aggregated via errors.Join but never
// aborts the others or the daemon (research §1 / Pattern 2).
//
// The zero Registry is not usable — call NewRegistry (the channels/started maps
// must be non-nil). StartAll/StopAll are safe to call once each per registry;
// StopAll only stops channels StartAll actually started, and is idempotent.
type Registry struct {
	channels map[string]Channel

	// enabledOverride lets serve.go's --no-telegram / --only=cli flags (plan
	// 13-09) override the AURA_CHANNEL_<NAME>_ENABLED env gate. It returns
	// (enabled, ok): ok=false means "no override, fall back to env". nil means
	// no override is installed.
	enabledOverride func(name string) (enabled, ok bool)

	mu      sync.Mutex
	started map[string]Channel // channels StartAll actually started (StopAll target)
}

// NewRegistry returns an empty, ready-to-use Registry.
func NewRegistry() *Registry {
	return &Registry{
		channels: make(map[string]Channel),
		started:  make(map[string]Channel),
	}
}

// Register adds a channel under its Name. A later Register with the same Name
// replaces the earlier one (mirrors tools.Registry).
func (r *Registry) Register(c Channel) {
	r.channels[c.Name()] = c
}

// SetEnabledOverride installs the flag-driven override predicate. predicate(name)
// returns (enabled, ok); ok=false defers to the AURA_CHANNEL_<NAME>_ENABLED env
// gate. Passing nil clears the override.
func (r *Registry) SetEnabledOverride(predicate func(name string) (enabled, ok bool)) {
	r.enabledOverride = predicate
}

// StartAll starts every enabled channel. A channel is enabled when its override
// (if any) says so, else when AURA_CHANNEL_<upper(Name)>_ENABLED is not "false"
// (default true). Disabled channels are skipped (never started). Start failures
// are logged and aggregated with errors.Join — one failure never aborts the
// siblings (fail-soft). Started channels are tracked so StopAll stops only them.
func (r *Registry) StartAll(ctx context.Context) error {
	var errs []error
	for name, ch := range r.channels {
		if !r.enabled(name) {
			slog.Info("channels: channel disabled, skipping start", "channel", name)
			continue
		}
		if err := ch.Start(ctx); err != nil {
			// Fail-soft: log + aggregate, but keep starting the rest and keep the
			// daemon alive (mirrors serve.go agui http "log but never exit").
			slog.Error("channels: channel start failed", "channel", name, "err", err)
			errs = append(errs, fmt.Errorf("channels: start %q: %w", name, err))
			continue
		}
		r.mu.Lock()
		r.started[name] = ch
		r.mu.Unlock()
		slog.Info("channels: channel started", "channel", name)
	}
	return errors.Join(errs...)
}

// StopAll stops every channel StartAll started, aggregating drain errors with
// errors.Join. It is idempotent: a channel is removed from the started set once
// stopped, so a second StopAll (or one before StartAll) is a clean no-op.
func (r *Registry) StopAll(ctx context.Context) error {
	r.mu.Lock()
	toStop := make(map[string]Channel, len(r.started))
	for name, ch := range r.started {
		toStop[name] = ch
	}
	r.mu.Unlock()

	var errs []error
	for name, ch := range toStop {
		if err := ch.Stop(ctx); err != nil {
			slog.Error("channels: channel stop failed", "channel", name, "err", err)
			errs = append(errs, fmt.Errorf("channels: stop %q: %w", name, err))
		}
		r.mu.Lock()
		delete(r.started, name)
		r.mu.Unlock()
	}
	return errors.Join(errs...)
}

// enabled resolves a channel's enablement: the override wins when it returns
// ok=true, else the AURA_CHANNEL_<upper(Name)>_ENABLED env gate (default true).
func (r *Registry) enabled(name string) bool {
	if r.enabledOverride != nil {
		if on, ok := r.enabledOverride(name); ok {
			return on
		}
	}
	return envChannelEnabled(name)
}

// envChannelEnabled reads AURA_CHANNEL_<upper(Name)>_ENABLED with the
// silent-fallback contract of config.envBoolDefault: unset/empty/malformed →
// default true (a registered channel runs unless deliberately disabled, T-13-04-
// EnableBypass accept disposition).
func envChannelEnabled(name string) bool {
	key := "AURA_CHANNEL_" + strings.ToUpper(name) + "_ENABLED"
	v := os.Getenv(key)
	if v == "" {
		return true
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return true
	}
	return b
}
