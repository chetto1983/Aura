package mcptools

import (
	"context"
	"log/slog"
	"time"

	"github.com/chetto1983/aura/internal/envutil"
	"github.com/chetto1983/aura/internal/mcp"
)

// bridge_ping.go adds a bounded background liveness poll on top of the reactive
// reconnect-on-transport-error machinery in bridge_reconnect.go: a dead server is
// otherwise discovered only when a tool call fails mid-run. A per-server poller
// goroutine periodically calls the mounted transport's Ping and, on a
// transport-classified failure, triggers the SAME reconnectAfterTransport a
// failed tool call would — the existing breaker (3 failures / 30s cooldown) and
// capped exponential backoff already bound a runaway reconnect storm, so a poll
// tick that lands while the breaker is open just observes that error and moves
// on to the next tick.

const envMCPPingIntervalSec = "AURA_MCP_PING_INTERVAL_SEC"

const defaultMCPPingIntervalSec = 60

// maxMCPPingTimeout bounds any single ping round-trip regardless of how long the
// configured interval is, so a slow/hanging server can never stall a poll tick
// past this ceiling.
const maxMCPPingTimeout = 10 * time.Second

// configuredMCPPingInterval resolves AURA_MCP_PING_INTERVAL_SEC once per mount,
// mirroring configuredMCPCallTimeout's package-local env-read convention
// (timeout.go): mcptools reads its own operative knobs directly rather than
// threading them through MountServer/MountManagedServer's signature. <=0
// disables the poller entirely (checked by the caller, startPingPoll).
func configuredMCPPingInterval() time.Duration {
	return time.Duration(envutil.IntDefault(envMCPPingIntervalSec, defaultMCPPingIntervalSec)) * time.Second
}

// startPingPoll starts the background poller when interval > 0. Both mount
// branches (mountStdio and the streamable-HTTP branch of MountManagedServer)
// call this right after a successful mountWithDefsPolicy, so a proactively-detected
// dead server self-heals via reconnectAfterTransport instead of waiting for the
// next tool call to fail. A no-op on an already-closed server (a mount whose
// registration failed after this would be reachable only via a future bug, but
// staying defensive costs nothing).
func (s *reconnectingServer) startPingPoll(interval time.Duration) {
	if interval <= 0 {
		return
	}
	timeout := min(interval, maxMCPPingTimeout)

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	processCtx := s.processCtx
	stop := make(chan struct{})
	done := make(chan struct{})
	s.pingStop = stop
	s.pingDone = done
	s.mu.Unlock()
	if processCtx == nil {
		processCtx = context.Background()
	}

	go s.pingLoop(processCtx, interval, timeout, stop, done)
}

func (s *reconnectingServer) pingLoop(processCtx context.Context, interval, timeout time.Duration, stop, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-processCtx.Done():
			return
		case <-ticker.C:
			s.pingOnce(processCtx, timeout)
		}
	}
}

// pingOnce runs one bounded liveness probe. A closed server (currentClient's
// error) is a silent no-op — the loop's next select observes stop or
// processCtx.Done() and exits on its own. Only transitions are logged (a
// transport failure triggering a reconnect, and that reconnect's own
// success/failure), never a healthy tick, per the poll's own no-spam contract.
func (s *reconnectingServer) pingOnce(ctx context.Context, timeout time.Duration) {
	client, err := s.currentClient()
	if err != nil {
		return
	}
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	err = client.Ping(pingCtx)
	cancel()
	if err == nil {
		return
	}
	if !mcp.IsTransportError(err) {
		slog.Warn("mcp ping failed", "server", s.name, "error", err)
		return
	}
	slog.Warn("mcp ping transport failure; triggering reconnect", "server", s.name, "error", err)
	if _, _, reconnectErr := s.reconnectAfterTransport(ctx, client); reconnectErr != nil {
		slog.Warn("mcp ping-triggered reconnect failed", "server", s.name, "error", reconnectErr)
		return
	}
	slog.Info("mcp ping-triggered reconnect succeeded", "server", s.name)
}
