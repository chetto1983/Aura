package mcptools

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/mcp"
)

type reconnectingClient interface {
	Server
	Ping(ctx context.Context) error
	Close() error
}

// openMCPClient is the two-context Open seam (Pitfall #2): processCtx bounds the
// reconnected subprocess's ENTIRE lifetime, handshakeCtx bounds only the
// initialize round-trip. Overridable in tests.
var openMCPClient = func(processCtx, handshakeCtx context.Context, name string, cfg mcp.ServerConfig) (reconnectingClient, error) {
	return mcp.OpenWithHandshakeContext(processCtx, handshakeCtx, name, cfg)
}

const (
	defaultMCPReconnectTimeout = 10 * time.Second
	mcpReconnectBreakerAfter   = 3
	mcpReconnectCooldown       = 30 * time.Second
	mcpReconnectBackoffMin     = 500 * time.Millisecond
	mcpReconnectBackoffMax     = 30 * time.Second
)

type reconnectingServer struct {
	mu                sync.Mutex
	name              string
	cfg               mcp.ServerConfig
	client            reconnectingClient
	bridged           map[string]*bridgedTool
	acceptedToolNames map[string]struct{}
	toolSetDriftErr   error
	refreshHook       func()
	closed            bool
	reconnectTimeout  time.Duration
	// open is the pluggable replacement-open seam: openReplacement calls s.open
	// instead of openMCPClient directly, so the SAME breaker/backoff/two-context
	// reopen/no-replay machinery serves both mount branches. The stdio
	// constructor path below defaults it to the existing openMCPClient var seam
	// (kept so tests that stub openMCPClient keep working unmodified);
	// MountManagedServer's streamable-HTTP branch overrides it via setOpen to
	// close over (name, server) and call mcp.OpenServer — an HTTP client has no
	// subprocess, so its open ignores processCtx but keeps the same signature.
	open func(processCtx, handshakeCtx context.Context) (reconnectingClient, error)
	// processCtx bounds a RECONNECTED replacement subprocess's entire lifetime
	// (Pitfall #2): set via setProcessContext to the same daemon/boot context
	// MountServer/MountManagedServer used for the initial Open, so a reconnect never
	// spawns a subprocess whose lifetime is tied to the short, deferred-cancel
	// reconnect-handshake timeout. Defaults to context.Background() when unset (test
	// constructions that never reconnect a real subprocess).
	processCtx context.Context

	reconnecting        bool
	reconnectDone       chan struct{}
	lastReconnectDefs   []mcp.ToolDef
	lastReconnectClient reconnectingClient
	lastReconnectErr    error
	reconnectFailures   int
	breakerOpenUntil    time.Time
	nextReconnectAfter  time.Time

	// pingStop/pingDone bound the optional background liveness-poll goroutine
	// (bridge_ping.go): pingStop is closed exactly once by Close() (guarded by
	// the closed flag above, mirroring how the rest of Close() is idempotent);
	// pingDone is closed by the poller goroutine itself on return, so tests can
	// deterministically observe it stopping instead of racing goleak's own retry
	// window. Both stay nil when startPingPoll was never called (interval<=0 or
	// a test construction that never mounts through the poll-starting path).
	pingStop chan struct{}
	pingDone chan struct{}
}

func newReconnectingServer(name string, cfg mcp.ServerConfig, client reconnectingClient) *reconnectingServer {
	s := &reconnectingServer{
		name:              name,
		cfg:               cfg,
		client:            client,
		bridged:           map[string]*bridgedTool{},
		acceptedToolNames: map[string]struct{}{},
		reconnectTimeout:  defaultMCPReconnectTimeout,
	}
	s.open = func(processCtx, handshakeCtx context.Context) (reconnectingClient, error) {
		return openMCPClient(processCtx, handshakeCtx, s.name, s.cfg)
	}
	return s
}

// setOpen overrides the replacement-open seam (see the open field's doc
// comment). Called once, right after construction, before the server is
// exposed to concurrent callers — mirrors setProcessContext's contract.
func (s *reconnectingServer) setOpen(open func(processCtx, handshakeCtx context.Context) (reconnectingClient, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.open = open
}

func (s *reconnectingServer) openFunc() func(context.Context, context.Context) (reconnectingClient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.open
}

// setProcessContext records the process-lifetime context a reconnect must use to
// spawn (or open) the replacement transport. MountServer/MountManagedServer call
// this right after construction with the same processCtx they passed to the
// initial Open.
func (s *reconnectingServer) setProcessContext(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.processCtx = ctx
}

func (s *reconnectingServer) processContext() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.processCtx == nil {
		return context.Background()
	}
	return s.processCtx
}

func (s *reconnectingServer) ListTools(ctx context.Context) ([]mcp.ToolDef, error) {
	client, err := s.currentClient()
	if err != nil {
		return nil, err
	}
	defs, err := client.ListTools(ctx)
	if !mcp.IsTransportError(err) {
		return defs, err
	}
	defs, retry, err := s.reconnectAfterTransport(ctx, client)
	if err != nil || defs != nil {
		return defs, err
	}
	return retry.ListTools(ctx)
}

func (s *reconnectingServer) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	client, err := s.currentClient()
	if err != nil {
		return "", err
	}
	text, err := client.CallTool(ctx, name, args)
	if !mcp.IsTransportError(err) {
		return text, err
	}
	operation, hasOperation := idempotency.OperationFromContext(ctx)
	if hasOperation && operation.Key.Scope == idempotency.ScopeMCPTool {
		return "", fmt.Errorf("%w: mcp %q mutating call %q transport failed after send; not replayed or reconnected: %v", mcp.ErrTransport, s.name, name, err)
	}
	readOnly := s.toolIsReadOnly(name)
	if _, _, reconnectErr := s.reconnectAfterTransport(ctx, client); reconnectErr != nil {
		return "", reconnectErr
	}
	if !readOnly {
		return "", fmt.Errorf("%w: mcp %q call %q transport failed after send; reconnected but not replayed: %v", mcp.ErrTransport, s.name, name, err)
	}
	retry, currentErr := s.currentClient()
	if currentErr != nil {
		return "", currentErr
	}
	return retry.CallTool(ctx, name, args)
}

func (s *reconnectingServer) Ping(ctx context.Context) error {
	client, err := s.currentClient()
	if err != nil {
		return err
	}
	if err := client.Ping(ctx); !mcp.IsTransportError(err) {
		return err
	}
	defs, retry, err := s.reconnectAfterTransport(ctx, client)
	if err != nil {
		return err
	}
	if defs != nil {
		return nil
	}
	return retry.Ping(ctx)
}

// toolIsReadOnly fails closed for unknown/untracked tools. Only an advertised,
// currently read-only descriptor may reconnect and reissue automatically.
func (s *reconnectingServer) toolIsReadOnly(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	tool, ok := s.bridged[name]
	return ok && !tool.Spec().Mutating
}

func (s *reconnectingServer) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	client := s.client
	pingStop := s.pingStop
	s.mu.Unlock()
	if pingStop != nil {
		close(pingStop)
	}
	if client == nil {
		return nil
	}
	return client.Close()
}

func (s *reconnectingServer) trackBridgedTools(bridged []tools.Tool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range bridged {
		if bt, ok := t.(*bridgedTool); ok {
			s.bridged[bt.name] = bt
		}
	}
}

func (s *reconnectingServer) trackAcceptedDefs(defs []mcp.ToolDef) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acceptedToolNames = toolNameSet(defs)
}

func (s *reconnectingServer) validateReconnectToolSet(defs []mcp.ToolDef) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.validateReconnectToolSetLocked(defs)
}

func (s *reconnectingServer) validateReconnectToolSetLocked(defs []mcp.ToolDef) error {
	if len(s.acceptedToolNames) == 0 {
		return nil
	}
	next := toolNameSet(defs)
	if len(next) != len(defs) || len(next) != len(s.acceptedToolNames) {
		return fmt.Errorf("%w: mcp %q tool set changed; restart required", mcp.ErrTransport, s.name)
	}
	for name := range s.acceptedToolNames {
		if _, ok := next[name]; !ok {
			return fmt.Errorf("%w: mcp %q tool set changed; restart required", mcp.ErrTransport, s.name)
		}
	}
	return nil
}

func toolNameSet(defs []mcp.ToolDef) map[string]struct{} {
	names := make(map[string]struct{}, len(defs))
	for _, def := range defs {
		names[def.Name] = struct{}{}
	}
	return names
}

func (s *reconnectingServer) setRefreshHook(hook func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshHook = hook
}

func (s *reconnectingServer) refreshSpecsLocked(defs []mcp.ToolDef) {
	changed := false
	for _, d := range defs {
		if bt, ok := s.bridged[d.Name]; ok {
			bt.refreshSpec(d)
			changed = true
		}
	}
	if changed && s.refreshHook != nil {
		s.refreshHook()
	}
}

func (s *reconnectingServer) currentClient() (reconnectingClient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.toolSetDriftErr != nil {
		return nil, s.toolSetDriftErr
	}
	if s.closed || s.client == nil {
		return nil, fmt.Errorf("%w: mcp %q is closed", mcp.ErrTransport, s.name)
	}
	return s.client, nil
}

func (s *reconnectingServer) reconnectAfterTransport(
	ctx context.Context, failed reconnectingClient,
) ([]mcp.ToolDef, reconnectingClient, error) {
	for {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return nil, nil, fmt.Errorf("%w: mcp %q is closed", mcp.ErrTransport, s.name)
		}
		if s.client != failed {
			retry := s.client
			s.mu.Unlock()
			if retry == nil {
				return nil, nil, fmt.Errorf("%w: mcp %q has no client", mcp.ErrTransport, s.name)
			}
			return nil, retry, nil
		}
		if s.client == nil {
			s.mu.Unlock()
			return nil, nil, fmt.Errorf("%w: mcp %q has no client", mcp.ErrTransport, s.name)
		}
		now := time.Now()
		if err := s.reconnectBreakerErrorLocked(now); err != nil {
			s.mu.Unlock()
			return nil, nil, err
		}
		if s.reconnecting {
			done := s.reconnectDone
			s.mu.Unlock()
			return s.waitForReconnect(ctx, done)
		}
		if delay := time.Until(s.nextReconnectAfter); delay > 0 {
			s.mu.Unlock()
			if err := sleepContext(ctx, delay); err != nil {
				return nil, nil, err
			}
			continue
		}

		done := make(chan struct{})
		timeout := s.reconnectTimeout
		s.reconnecting = true
		s.reconnectDone = done
		s.mu.Unlock()

		defs, next, err := s.openReplacement(ctx, timeout)

		var closeFailed bool
		var closeNext reconnectingClient
		s.mu.Lock()
		if s.closed {
			if next != nil {
				closeNext = next
			}
			defs = nil
			next = nil
			err = fmt.Errorf("%w: mcp %q is closed", mcp.ErrTransport, s.name)
		} else if err == nil {
			if driftErr := s.validateReconnectToolSetLocked(defs); driftErr != nil {
				closeNext = next
				defs = nil
				next = nil
				err = driftErr
				s.toolSetDriftErr = driftErr
			} else {
				s.client = next
				s.refreshSpecsLocked(defs)
				s.reconnectFailures = 0
				s.breakerOpenUntil = time.Time{}
				s.nextReconnectAfter = time.Time{}
				closeFailed = failed != nil && failed != next
			}
		}
		if err != nil && !s.closed {
			s.recordReconnectFailureLocked(time.Now(), err)
		}
		s.lastReconnectDefs = defs
		s.lastReconnectClient = next
		s.lastReconnectErr = err
		s.reconnecting = false
		close(done)
		s.mu.Unlock()

		if closeFailed {
			_ = failed.Close()
		}
		if closeNext != nil {
			_ = closeNext.Close()
		}
		return defs, next, err
	}
}

// openReplacement opens a replacement transport and lists its tools. It is
// Pitfall #2-safe: handshakeCtx (derived from parent, bounded by timeout, and
// deferred-canceled at return) bounds ONLY the open handshake + tools/list —
// openMCPClient's process-lifetime argument is s.processContext(), a long-lived
// context that is NOT canceled when this function returns, so the just-opened
// replacement subprocess survives past openReplacement's own deferred cancel
// (previously this function passed the SAME bounded, defer-canceled context as
// both the handshake bound and exec.CommandContext's process-lifetime context —
// the deferred cancel would fire and kill the freshly-reconnected subprocess the
// instant openReplacement returned).
func (s *reconnectingServer) openReplacement(parent context.Context, timeout time.Duration) ([]mcp.ToolDef, reconnectingClient, error) {
	if timeout <= 0 {
		timeout = defaultMCPReconnectTimeout
	}
	handshakeCtx, cancel := context.WithTimeout(context.WithoutCancel(parent), timeout)
	defer cancel()

	next, err := s.openFunc()(s.processContext(), handshakeCtx)
	if err != nil {
		return nil, nil, fmt.Errorf("reconnect mcp %q: %w", s.name, err)
	}
	defs, err := next.ListTools(handshakeCtx)
	if err != nil {
		_ = next.Close()
		return nil, nil, err
	}
	return defs, next, nil
}

func (s *reconnectingServer) waitForReconnect(
	ctx context.Context,
	done <-chan struct{},
) ([]mcp.ToolDef, reconnectingClient, error) {
	select {
	case <-done:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.lastReconnectDefs, s.lastReconnectClient, s.lastReconnectErr
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
}

func (s *reconnectingServer) reconnectBreakerErrorLocked(now time.Time) error {
	if s.breakerOpenUntil.IsZero() || !now.Before(s.breakerOpenUntil) {
		return nil
	}
	return fmt.Errorf("%w: mcp %q reconnect breaker open until %s", mcp.ErrTransport, s.name, s.breakerOpenUntil.Format(time.RFC3339))
}

func (s *reconnectingServer) recordReconnectFailureLocked(now time.Time, err error) {
	s.reconnectFailures++
	backoff := reconnectBackoff(s.reconnectFailures)
	s.nextReconnectAfter = now.Add(backoff)
	if s.reconnectFailures >= mcpReconnectBreakerAfter {
		s.breakerOpenUntil = now.Add(mcpReconnectCooldown)
	}
	slog.Warn("mcp reconnect failed",
		"server", s.name,
		"failures", s.reconnectFailures,
		"backoff", backoff.String(),
		"breaker_open_until", s.breakerOpenUntil.Format(time.RFC3339),
		"error", err,
	)
}

func reconnectBackoff(failures int) time.Duration {
	if failures <= 0 {
		return 0
	}
	delay := mcpReconnectBackoffMin
	for i := 1; i < failures && delay < mcpReconnectBackoffMax; i++ {
		delay *= 2
		if delay > mcpReconnectBackoffMax {
			return mcpReconnectBackoffMax
		}
	}
	return delay
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
