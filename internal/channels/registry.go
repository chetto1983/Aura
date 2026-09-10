package channels

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/chetto1983/aura/internal/envutil"
)

// ErrChannelDisabled reports a channel its enable gate keeps off: the --no-telegram /
// --only=cli override or AURA_CHANNEL_<NAME>_ENABLED=false.
var ErrChannelDisabled = errors.New("channel disabled")

// ErrRegistryStopped reports a Replace that arrived after StopAll began the shutdown.
var ErrRegistryStopped = errors.New("channel registry stopped")

// Registry holds the set of daemon channels and aggregates their lifecycle. It
// mirrors the map-backed internal/agent/tools.Registry idiom (NewRegistry /
// Register) and the fail-soft daemon-subsystem posture of bootServe: one
// channel's Start failure is logged and aggregated via errors.Join but never
// aborts the others or the daemon (research §1 / Pattern 2).
//
// The zero Registry is not usable — call NewRegistry. Every method is safe for
// concurrent use. lifecycle serialises the start/stop transitions (StartAll,
// StopAll, Replace, Stop) and is held across the blocking Channel.Start/Stop calls,
// so two swaps of one name never overlap; mu guards only the maps and the override
// and is never held across a network call, so a delivery never waits on a swap.
type Registry struct {
	lifecycle sync.Mutex
	// stopped is set by StopAll. drainShutdown stops the channels before the HTTP
	// server, so a settings write can still reach Replace afterwards; refusing it keeps
	// a poller from starting that nothing will ever stop.
	stopped bool

	mu       sync.Mutex
	channels map[string]Channel
	started  map[string]Channel // channels actually started (the StopAll target)
	// enabledOverride lets serve.go's --no-telegram / --only=cli flags (plan
	// 13-09) override the AURA_CHANNEL_<NAME>_ENABLED env gate. It returns
	// (enabled, ok): ok=false means "no override, fall back to env". nil means
	// no override is installed.
	enabledOverride func(name string) (enabled, ok bool)
}

// NewRegistry returns an empty, ready-to-use Registry.
func NewRegistry() *Registry {
	return &Registry{
		channels: make(map[string]Channel),
		started:  make(map[string]Channel),
	}
}

// Register adds a channel under its Name. A later Register with the same Name
// replaces the earlier one (mirrors tools.Registry) without starting or stopping
// anything; Replace is the runtime swap.
func (r *Registry) Register(c Channel) {
	r.mu.Lock()
	r.channels[c.Name()] = c
	r.mu.Unlock()
}

// SetEnabledOverride installs the flag-driven override predicate. predicate(name)
// returns (enabled, ok); ok=false defers to the AURA_CHANNEL_<NAME>_ENABLED env
// gate. Passing nil clears the override.
func (r *Registry) SetEnabledOverride(predicate func(name string) (enabled, ok bool)) {
	r.mu.Lock()
	r.enabledOverride = predicate
	r.mu.Unlock()
}

// StartAll starts every enabled channel. A channel is enabled when its override
// (if any) says so, else when AURA_CHANNEL_<upper(Name)>_ENABLED is not "false"
// (default true). Disabled channels are skipped (never started). Start failures
// are logged and aggregated with errors.Join — one failure never aborts the
// siblings (fail-soft). Started channels are tracked so StopAll stops only them.
func (r *Registry) StartAll(ctx context.Context) error {
	r.lifecycle.Lock()
	defer r.lifecycle.Unlock()
	r.mu.Lock()
	toStart := maps.Clone(r.channels)
	r.mu.Unlock()

	var errs []error
	for name, ch := range toStart {
		if err := r.start(ctx, name, ch); err != nil && !errors.Is(err, ErrChannelDisabled) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// StopAll stops every started channel, aggregating drain errors with errors.Join.
// It is idempotent: a channel leaves the started set as it is stopped, so a second
// StopAll (or one before StartAll) is a clean no-op. It also ends the registry's
// runtime: a later Replace returns ErrRegistryStopped.
func (r *Registry) StopAll(ctx context.Context) error {
	r.lifecycle.Lock()
	defer r.lifecycle.Unlock()
	r.stopped = true
	r.mu.Lock()
	names := slices.Collect(maps.Keys(r.started))
	r.mu.Unlock()

	var errs []error
	for _, name := range names {
		if err := r.stop(ctx, name); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Replace swaps the channel registered under c's Name at runtime: the instance
// started under that name (if any) is drained as StopAll drains it, c is
// registered, and c is started through the same enable gate as StartAll. It
// returns nil only when c is running. A disabled c returns ErrChannelDisabled; a
// failed Start is logged and returned, leaving c registered but not started
// (fail-soft); after StopAll it returns ErrRegistryStopped and changes nothing.
// A drain failure of the old instance is logged, not returned: it has left the
// started set either way, and the error reports whether c runs. ctx reaches c.Start,
// which may keep it for the channel's lifetime (Telegram parents every turn on it), so
// pass the daemon's context, never a request's.
func (r *Registry) Replace(ctx context.Context, c Channel) error {
	name := c.Name()
	r.lifecycle.Lock()
	defer r.lifecycle.Unlock()
	if r.stopped {
		return fmt.Errorf("channels: replace %q: %w", name, ErrRegistryStopped)
	}
	_ = r.stop(ctx, name) // logged inside; see the doc comment
	r.mu.Lock()
	r.channels[name] = c
	r.mu.Unlock()
	return r.start(ctx, name, c)
}

// Stop drains the channel started under name, leaving it registered. A name with
// nothing started is a no-op.
func (r *Registry) Stop(ctx context.Context, name string) error {
	r.lifecycle.Lock()
	defer r.lifecycle.Unlock()
	return r.stop(ctx, name)
}

// start runs one channel through the enable gate and Start, recording it as
// started on success. The caller holds lifecycle.
func (r *Registry) start(ctx context.Context, name string, ch Channel) error {
	if !r.enabled(name) {
		slog.Info("channels: channel disabled, skipping start", "channel", name)
		return fmt.Errorf("channels: start %q: %w", name, ErrChannelDisabled)
	}
	if err := ch.Start(ctx); err != nil {
		// Fail-soft: log + return, but keep the daemon alive (mirrors serve.go agui
		// http "log but never exit").
		slog.Error("channels: channel start failed", "channel", name, "err", err)
		return fmt.Errorf("channels: start %q: %w", name, err)
	}
	r.mu.Lock()
	r.started[name] = ch
	r.mu.Unlock()
	slog.Info("channels: channel started", "channel", name)
	return nil
}

// stop drops the channel started under name from the started set, then drains it.
// It leaves the set first so no delivery reaches a draining channel, and stays out
// even when Stop fails (a channel that failed to drain is not retried). The caller
// holds lifecycle.
func (r *Registry) stop(ctx context.Context, name string) error {
	r.mu.Lock()
	ch, ok := r.started[name]
	delete(r.started, name)
	r.mu.Unlock()
	if !ok {
		return nil
	}
	if err := ch.Stop(ctx); err != nil {
		slog.Error("channels: channel stop failed", "channel", name, "err", err)
		return fmt.Errorf("channels: stop %q: %w", name, err)
	}
	return nil
}

// DeliverToIdentity fans a push out to the started channel that owns identityID,
// in a deterministic sorted-by-name order (Phase 20 Fork 4 / D-05 — NEVER Go map
// iteration order, which is nondeterministic the moment a 2nd Deliverer lands).
// A started Channel that does not implement Deliverer is skipped (it cannot push).
// The tri-state Deliverer contract drives the fan-out: the first channel to
// deliver wins (returns true,nil); an owning channel that fails stops the fan-out
// with (false, err) and never asks a sibling (no double-delivery); when no channel
// owns the identity it returns (false, nil) so the caller falls back to its route.
//
// The lock is held only to snapshot r.started — a Deliver call can block on the
// network, so it runs unlocked.
func (r *Registry) DeliverToIdentity(ctx context.Context, identityID, text string) (bool, error) {
	names, snap := r.startedSnapshot()
	for _, n := range names {
		d, ok := snap[n].(Deliverer)
		if !ok {
			continue // channel can't push → skip (zero change for a new channel)
		}
		delivered, err := d.Deliver(ctx, identityID, text)
		if err != nil {
			return false, err // owns-but-failed → stop, no siblings
		}
		if delivered {
			return true, nil // first-delivers-wins
		}
	}
	return false, nil // not-my-user across all → caller falls back to the route
}

// DeliverToConversation fans a push out only to a started channel that proves
// ownership of both identityID and conversationID. It deliberately does not
// fall back to Deliverer: doing so would recreate identity-wide cross-channel
// routing for work that originated in a cockpit or CLI conversation.
func (r *Registry) DeliverToConversation(ctx context.Context, identityID, conversationID, text string) (bool, error) {
	if identityID == "" || conversationID == "" {
		return false, nil
	}
	names, snap := r.startedSnapshot()
	for _, n := range names {
		d, ok := snap[n].(ConversationDeliverer)
		if !ok {
			continue
		}
		delivered, err := d.DeliverConversation(ctx, identityID, conversationID, text)
		if err != nil {
			return false, err
		}
		if delivered {
			return true, nil
		}
	}
	return false, nil
}

// startedSnapshot copies the started set under mu and returns its names sorted, so
// the delivery fan-outs iterate a stable order without holding the lock.
func (r *Registry) startedSnapshot() ([]string, map[string]Channel) {
	r.mu.Lock()
	snap := maps.Clone(r.started)
	r.mu.Unlock()
	return slices.Sorted(maps.Keys(snap)), snap
}

// DeliverApproval fans an ACTIONABLE approval prompt out to the started channel that owns
// identityID, in the SAME deterministic sorted-by-name order and tri-state contract as
// DeliverToIdentity (Amendment #92 revised): a started Channel that does not implement
// ApprovalDeliverer is skipped; the first channel to deliver wins; an owning channel that fails
// stops the fan-out (no sibling double-delivery); no owner → (false, nil) so the caller falls
// back (for an approval that means the WebUI pull surface handles it, not an error). The token
// binds the inline buttons to the pending ask_user pause the operator resolves; taskID/kind feed
// the bounded, secret-safe prompt.
func (r *Registry) DeliverApproval(ctx context.Context, identityID, token, taskID, kind string) (bool, error) {
	names, snap := r.startedSnapshot()
	for _, n := range names {
		d, ok := snap[n].(ApprovalDeliverer)
		if !ok {
			continue // channel can't render an actionable approval → skip
		}
		delivered, err := d.DeliverApproval(ctx, identityID, token, taskID, kind)
		if err != nil {
			return false, err // owns-but-failed → stop, no siblings
		}
		if delivered {
			return true, nil // first-delivers-wins
		}
	}
	return false, nil // not-my-user across all → caller falls back (WebUI pulls the pause)
}

// enabled resolves a channel's enablement: the override wins when it returns
// ok=true, else the AURA_CHANNEL_<upper(Name)>_ENABLED env gate (default true).
func (r *Registry) enabled(name string) bool {
	r.mu.Lock()
	override := r.enabledOverride
	r.mu.Unlock()
	if override != nil {
		if on, ok := override(name); ok {
			return on
		}
	}
	return envChannelEnabled(name)
}

// envChannelEnabled reads AURA_CHANNEL_<upper(Name)>_ENABLED with the
// silent-fallback contract of envutil.BoolDefault: unset/empty/malformed →
// default true (a registered channel runs unless deliberately disabled, T-13-04-
// EnableBypass accept disposition).
func envChannelEnabled(name string) bool {
	key := "AURA_CHANNEL_" + strings.ToUpper(name) + "_ENABLED"
	return envutil.BoolDefault(key, true)
}
