package mcptools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
)

// pingFakeClient is a reconnectingClient double whose Ping result is scripted
// per-call (mirroring errReconnectClient's ListTools/CallTool scripting), so the
// ping-poll's transport-vs-non-transport and healthy-vs-failing branches are
// driven without a process.
type pingFakeClient struct {
	defs      []mcp.ToolDef
	callText  string
	callErr   error
	pingErrs  []error
	pingN     atomic.Int32
	pingCalls atomic.Int32
	closed    atomic.Bool
}

func (c *pingFakeClient) ListTools(context.Context) ([]mcp.ToolDef, error) { return c.defs, nil }

func (c *pingFakeClient) CallTool(context.Context, string, map[string]any) (string, error) {
	return c.callText, c.callErr
}

func (c *pingFakeClient) Close() error {
	c.closed.Store(true)
	return nil
}

func (c *pingFakeClient) Ping(context.Context) error {
	i := int(c.pingN.Add(1)) - 1
	c.pingCalls.Add(1)
	if i < len(c.pingErrs) {
		return c.pingErrs[i]
	}
	return nil
}

// waitForCondition polls cond every 2ms up to 2s, failing the test if it never
// becomes true — used instead of a fixed sleep so the short-interval poller
// tests stay fast and non-flaky under load.
func waitForCondition(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal(msg)
}

// TestReconnectingServer_SetOpenGeneralizesReplacementOpen proves openReplacement
// calls the pluggable s.open seam (set via setOpen) instead of the package-level
// openMCPClient var — the mechanism MountManagedServer's HTTP branch relies on to
// reuse the stdio reconnect machinery unchanged. Mirrors
// TestReconnectServerSecondTransportFailureIsInlineToolError's mutating no-replay
// shape, but drives the reconnect via setOpen and asserts openMCPClient itself is
// never invoked.
func TestReconnectingServer_SetOpenGeneralizesReplacementOpen(t *testing.T) {
	oldOpen := openMCPClient
	t.Cleanup(func() { openMCPClient = oldOpen })
	openMCPClient = func(context.Context, context.Context, string, mcp.ServerConfig) (reconnectingClient, error) {
		t.Fatal("setOpen must be consulted instead of the default openMCPClient var")
		return nil, nil
	}

	initial := &fakeReconnectClient{
		defs:    []mcp.ToolDef{{Name: "send"}},
		callErr: fmt.Errorf("session dropped: %w", mcp.ErrTransport),
	}
	reopened := &fakeReconnectClient{defs: []mcp.ToolDef{{Name: "send"}}, callText: "sent"}
	var opens atomic.Int32
	srv := newReconnectingServer("http-mail", mcp.ServerConfig{}, initial)
	srv.setOpen(func(processCtx, handshakeCtx context.Context) (reconnectingClient, error) {
		opens.Add(1)
		if processCtx == nil || handshakeCtx == nil {
			t.Fatal("setOpen closure must receive non-nil contexts")
		}
		return reopened, nil
	})

	ctx := tools.WithToolCallContext(context.Background(), "sess", "call-1", t.TempDir(), 2048)
	if _, err := srv.CallTool(ctx, "send", nil); !mcp.IsTransportError(err) || !strings.Contains(err.Error(), "not replayed") {
		t.Fatalf("first CallTool = %v, want the no-replay transport error", err)
	}
	if opens.Load() != 1 {
		t.Fatalf("want exactly 1 replacement open via the pluggable seam, got %d", opens.Load())
	}
	if !initial.closed {
		t.Fatal("the failed client must be closed after a successful reconnect")
	}

	ctx2 := tools.WithToolCallContext(context.Background(), "sess", "call-2", t.TempDir(), 2048)
	text, err := srv.CallTool(ctx2, "send", nil)
	if err != nil {
		t.Fatalf("second CallTool should be served by the reconnected client: %v", err)
	}
	if text != "sent" || reopened.callCount != 1 {
		t.Fatalf("text=%q reopened.callCount=%d, want sent/1", text, reopened.callCount)
	}
}

// TestConfiguredMCPPingInterval covers the env-read convention: unset defaults to
// 60s, a valid override applies, and <=0 disables (startPingPoll's caller-side
// check, not this function's job to clamp).
func TestConfiguredMCPPingInterval(t *testing.T) {
	if got := configuredMCPPingInterval(); got != defaultMCPPingIntervalSec*time.Second {
		t.Fatalf("unset AURA_MCP_PING_INTERVAL_SEC = %v, want default %v", got, defaultMCPPingIntervalSec*time.Second)
	}
	t.Setenv(envMCPPingIntervalSec, "15")
	if got := configuredMCPPingInterval(); got != 15*time.Second {
		t.Fatalf("AURA_MCP_PING_INTERVAL_SEC=15 = %v, want 15s", got)
	}
	t.Setenv(envMCPPingIntervalSec, "0")
	if got := configuredMCPPingInterval(); got != 0 {
		t.Fatalf("AURA_MCP_PING_INTERVAL_SEC=0 = %v, want 0 (disables in startPingPoll)", got)
	}
	t.Setenv(envMCPPingIntervalSec, "-5")
	if got := configuredMCPPingInterval(); got != -5*time.Second {
		t.Fatalf("AURA_MCP_PING_INTERVAL_SEC=-5 = %v, want -5s (disables in startPingPoll)", got)
	}
}

// TestReconnectingServer_StartPingPollDisabledWhenNonPositive covers
// startPingPoll's interval<=0 early return: no poller goroutine, no pingStop/
// pingDone allocated.
func TestReconnectingServer_StartPingPollDisabledWhenNonPositive(t *testing.T) {
	for _, interval := range []time.Duration{0, -time.Second} {
		srv := newReconnectingServer("mail", mcp.ServerConfig{Command: "fake"}, &pingFakeClient{})
		srv.startPingPoll(interval)
		srv.mu.Lock()
		stop, done := srv.pingStop, srv.pingDone
		srv.mu.Unlock()
		if stop != nil || done != nil {
			t.Fatalf("interval=%v must not start a poller, got stop=%v done=%v", interval, stop != nil, done != nil)
		}
		if err := srv.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}
}

// TestReconnectingServer_StartPingPollNoopWhenClosed covers the defensive
// already-closed branch: starting the poller on a closed server is a no-op.
func TestReconnectingServer_StartPingPollNoopWhenClosed(t *testing.T) {
	srv := newReconnectingServer("mail", mcp.ServerConfig{Command: "fake"}, &pingFakeClient{})
	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	srv.startPingPoll(time.Millisecond)
	srv.mu.Lock()
	stop := srv.pingStop
	srv.mu.Unlock()
	if stop != nil {
		t.Fatal("startPingPoll on an already-closed server must not start a poller")
	}
}

// TestReconnectingServer_PingPollTriggersReconnectOnTransportError covers the
// proactive self-heal path: a ping classified as a transport error triggers
// reconnectAfterTransport WITHOUT any tool call, and the replacement client
// opened via the pluggable seam is swapped in.
func TestReconnectingServer_PingPollTriggersReconnectOnTransportError(t *testing.T) {
	initial := &pingFakeClient{
		defs:     []mcp.ToolDef{{Name: "lookup"}},
		pingErrs: []error{fmt.Errorf("dead session: %w", mcp.ErrTransport)},
	}
	fresh := &pingFakeClient{defs: []mcp.ToolDef{{Name: "lookup"}}}
	var opens atomic.Int32
	srv := newReconnectingServer("http-catalog", mcp.ServerConfig{}, initial)
	srv.setProcessContext(context.Background())
	srv.setOpen(func(context.Context, context.Context) (reconnectingClient, error) {
		opens.Add(1)
		return fresh, nil
	})

	srv.startPingPoll(2 * time.Millisecond)
	t.Cleanup(func() {
		if err := srv.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	waitForCondition(t, func() bool { return opens.Load() == 1 }, "ping-triggered reconnect never opened a replacement")
	waitForCondition(t, func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return srv.client == fresh
	}, "the reconnected client was never swapped into s.client")
	if !initial.closed.Load() {
		t.Fatal("the dead client must be closed after a ping-triggered reconnect")
	}

	// The fresh client's Ping never errors, so no further reconnect fires on
	// later ticks: give the poller a few more ticks and confirm opens stays 1.
	// srv.CallTool is never invoked anywhere in this test, so the reconnect
	// above is structurally proactive (ping-only), not tool-call-triggered.
	time.Sleep(20 * time.Millisecond)
	if got := opens.Load(); got != 1 {
		t.Fatalf("want exactly 1 reconnect (breaker/backoff-bounded), got %d opens", got)
	}
}

// TestReconnectingServer_PingPollHealthyNoReconnect covers the healthy-ping
// no-op path: a client whose Ping always succeeds never triggers the opener,
// across several poll ticks.
func TestReconnectingServer_PingPollHealthyNoReconnect(t *testing.T) {
	client := &pingFakeClient{defs: []mcp.ToolDef{{Name: "lookup"}}}
	var opens atomic.Int32
	srv := newReconnectingServer("http-catalog", mcp.ServerConfig{}, client)
	srv.setProcessContext(context.Background())
	srv.setOpen(func(context.Context, context.Context) (reconnectingClient, error) {
		opens.Add(1)
		return client, nil
	})

	srv.startPingPoll(2 * time.Millisecond)
	waitForCondition(t, func() bool { return client.pingCalls.Load() >= 3 }, "healthy poller never ticked")
	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if opens.Load() != 0 {
		t.Fatalf("a healthy ping must never trigger a reconnect, got %d opens", opens.Load())
	}
}

// TestReconnectingServer_PingPollNonTransportErrorNoReconnect covers the
// !IsTransportError branch: a plain ping failure (e.g. a malformed response) is
// logged only, never triggers reconnectAfterTransport.
func TestReconnectingServer_PingPollNonTransportErrorNoReconnect(t *testing.T) {
	plain := errors.New("ping: malformed response")
	client := &pingFakeClient{
		defs:     []mcp.ToolDef{{Name: "lookup"}},
		pingErrs: []error{plain, plain, plain},
	}
	var opens atomic.Int32
	srv := newReconnectingServer("http-catalog", mcp.ServerConfig{}, client)
	srv.setProcessContext(context.Background())
	srv.setOpen(func(context.Context, context.Context) (reconnectingClient, error) {
		opens.Add(1)
		return client, nil
	})

	srv.startPingPoll(2 * time.Millisecond)
	waitForCondition(t, func() bool { return client.pingCalls.Load() >= 3 }, "non-transport-error poller never ticked enough")
	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if opens.Load() != 0 {
		t.Fatalf("a non-transport ping error must NOT trigger a reconnect, got %d opens", opens.Load())
	}
}

// TestReconnectingServer_PingPollStopsOnClose covers the poller's lifetime
// contract: after Close, no further Ping call is observed, proving the stop
// channel actually halts the goroutine rather than merely being ignored (the
// package's goleak harness in main_test.go independently catches a genuinely
// leaked goroutine at suite end).
func TestReconnectingServer_PingPollStopsOnClose(t *testing.T) {
	client := &pingFakeClient{defs: []mcp.ToolDef{{Name: "lookup"}}}
	srv := newReconnectingServer("http-catalog", mcp.ServerConfig{}, client)
	srv.setProcessContext(context.Background())
	srv.setOpen(func(context.Context, context.Context) (reconnectingClient, error) {
		return client, nil
	})

	srv.startPingPoll(2 * time.Millisecond)
	waitForCondition(t, func() bool { return client.pingCalls.Load() >= 2 }, "poller never ticked before Close")

	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	afterClose := client.pingCalls.Load()
	time.Sleep(30 * time.Millisecond)
	if got := client.pingCalls.Load(); got != afterClose {
		t.Fatalf("ping calls kept increasing after Close: before=%d after=%d — poller did not stop", afterClose, got)
	}
}

// TestReconnectingServer_PingPollDerivesFromProcessCtx covers the OTHER stop
// path: cancelling the server's processCtx (the daemon shutting down) must also
// halt the poller, independent of an explicit Close call.
func TestReconnectingServer_PingPollDerivesFromProcessCtx(t *testing.T) {
	client := &pingFakeClient{defs: []mcp.ToolDef{{Name: "lookup"}}}
	processCtx, cancel := context.WithCancel(context.Background())
	srv := newReconnectingServer("http-catalog", mcp.ServerConfig{}, client)
	srv.setProcessContext(processCtx)
	srv.setOpen(func(context.Context, context.Context) (reconnectingClient, error) {
		return client, nil
	})

	srv.startPingPoll(2 * time.Millisecond)
	waitForCondition(t, func() bool { return client.pingCalls.Load() >= 2 }, "poller never ticked before processCtx cancel")

	cancel()
	waitForCondition(t, func() bool {
		before := client.pingCalls.Load()
		time.Sleep(15 * time.Millisecond)
		return client.pingCalls.Load() == before
	}, "poller kept ticking after processCtx was cancelled")

	if err := srv.Close(); err != nil {
		t.Fatalf("Close after processCtx cancel: %v", err)
	}
}
