package mcptools

import (
	"context"
	"log/slog"
	"time"

	"github.com/chetto1983/aura/internal/mcp"
)

// mount_retry.go bounds a TRANSIENT mount failure at boot so a managed sidecar that is briefly
// unreachable when Aura starts self-heals instead of leaving the agent with zero of that server's
// tools until the next full restart.
//
// Why this exists, and why it is SYNCHRONOUS: the boot race that left the memory MCP at 0 tools
// (the sidecar was still starting when Aura mounted it → connection refused) can recur on any
// restart path that compose's depends_on does NOT order — `docker restart aura` alone, or
// `docker compose restart`, neither of which waits for a dependency's healthcheck. The in-process
// mount has no other recovery: the agent's tools.Registry is immutable once a run starts (no lock;
// late Register would data-race the live dispatch), so tools cannot be registered asynchronously
// after boot, and reconnect-on-use only recovers a server that mounted at least once. A bounded
// retry INSIDE the boot loop — before the registry is frozen and handed to the agent — closes that
// gap without an async supervisor and without touching the immutable-registry contract. Because it
// is synchronous, a successful late attempt returns its closer to the boot loop for normal shutdown
// collection; there is no async closer bookkeeping to get wrong.
//
// Only transport errors retry. A config/spawn/collision/auth error (which a retry could never fix)
// returns immediately, preserving fail-fast for genuine misconfiguration and fail-soft overall
// (the boot loop still WARN-drops a server whose budget is spent).

// MountRetryPolicy bounds the boot-time retry of a transient mount failure. Attempts is the total
// number of tries (1 = the original single-shot behavior, i.e. retry disabled); the backoff between
// tries is capped exponential (BaseDelay·2^n clamped to MaxDelay). Zero/negative fields fall back to
// safe defaults so a partially-populated policy can never busy-loop or disable the bound.
type MountRetryPolicy struct {
	Attempts  int
	BaseDelay time.Duration
	MaxDelay  time.Duration
}

// MountWithRetry runs mount under policy, retrying ONLY transport failures with capped exponential
// backoff until it succeeds, exhausts the attempt budget, hits a non-transport error, or ctx is
// cancelled. The mount closure is the unit retried (MountManagedServer or MountServer), so a
// successful attempt returns its closer + mounted tool names to the caller unchanged. A failed
// transport attempt leaves the registry untouched — the transport dies during initialize, before
// any tool is registered — so a retry can never double-register a tool.
//
// D-06/D-07 (38-05): the caller (buildRegistryWithMCP) passes a BOUNDED handshake ctx here —
// distinct from the daemon-lifetime process ctx threaded separately through mount's closure — so
// this ctx caps the WHOLE retry budget (every attempt plus every backoff sleep), not one attempt
// individually: once the deadline elapses, the in-flight attempt's own transport read times out,
// and the very next sleepContext call observes ctx already Done and returns immediately, ending
// the loop well before Attempts is exhausted. A healthy mount that succeeds before the deadline is
// unaffected — the mounted server's subprocess lives on the separate, un-bounded process ctx.
func MountWithRetry(
	ctx context.Context,
	name string,
	policy MountRetryPolicy,
	mount func(context.Context) (func() error, []string, error),
) (func() error, []string, error) {
	attempts := max(policy.Attempts, 1)
	var (
		closer func() error
		names  []string
		err    error
	)
	for attempt := range attempts {
		closer, names, err = mount(ctx)
		if err == nil {
			return closer, names, nil
		}
		if attempt == attempts-1 || !mcp.IsTransportError(err) {
			return nil, nil, err
		}
		delay := mountRetryDelay(attempt, policy)
		slog.Warn("mcp mount transient failure; retrying",
			"server", name, "attempt", attempt+1, "attempts", attempts, "backoff", delay.String(), "err", err)
		if waitErr := sleepContext(ctx, delay); waitErr != nil {
			return nil, nil, waitErr
		}
	}
	return nil, nil, err
}

// mountRetryDelay is capped exponential backoff: BaseDelay·2^attempt, clamped to MaxDelay. Both
// bounds fall back to defaults when non-positive so a zero-value policy still backs off sanely.
func mountRetryDelay(attempt int, policy MountRetryPolicy) time.Duration {
	delay := policy.BaseDelay
	if delay <= 0 {
		delay = 500 * time.Millisecond
	}
	maxDelay := policy.MaxDelay
	if maxDelay <= 0 {
		maxDelay = 5 * time.Second
	}
	if delay > maxDelay {
		return maxDelay
	}
	for range attempt {
		delay *= 2
		if delay >= maxDelay {
			return maxDelay
		}
	}
	return delay
}
