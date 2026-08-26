// Package mcptools's bridge_supervisor.go: MountedServer is the host's supervisor
// over one *sdkmcp.ClientSession — session ownership, Wait()-driven death
// detection, redial policy, and tool-set drift. It intentionally contains no
// JSON-RPC framing, no session model and no wire handling: the SDK owns all of
// that. What remains is the one thing no MCP library can decide for a host —
// whether/when/how often to redial a dead session, and whether a mutating call
// that failed after send may ever be silently replayed. That policy is what
// survives from the deleted bridge_reconnect.go; its vocabulary is renamed
// reconnect->redial so the file reads as what it now is: a supervisor, not a
// second transport.
package mcptools

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/mcp"
)

// openSessionFunc is the pluggable session-open seam: mount.go fills it with a
// closure over mcp.OpenSDKSession (managed servers) or
// mcp.OpenSDKSessionForConfig (stdio), so MountedServer's redial path can reopen
// without depending on either directly.
type openSessionFunc func(processCtx, handshakeCtx context.Context, o mcp.SessionOptions) (*sdkmcp.ClientSession, error)

const (
	defaultMCPRedialTimeout = 10 * time.Second
	mcpRedialBreakerAfter   = 3
	mcpRedialCooldown       = 30 * time.Second
	mcpRedialBackoffMin     = 500 * time.Millisecond
	mcpRedialBackoffMax     = 30 * time.Second
)

// isSessionTransportFailure's jsonrpc-shutdown signal deliberately does NOT
// reach into go-sdk's internal/jsonrpc2 package: that package is unimportable
// from outside the SDK module (Go's internal-package visibility restricts it to
// importers rooted at github.com/modelcontextprotocol/go-sdk), and measured
// against the pinned v1.7.0 source it does not export a symbol named
// ErrClosed — an earlier draft of this plan cited `jsonrpc2.ErrClosed`, which
// does not exist at this version and could not be imported even if it did.
//
// What the SDK exports INSTEAD is better: mcp.ErrConnectionClosed
// (go-sdk@v1.7.0 mcp/transport.go:31-33), the sentinel its own generic call path
// wraps around EVERY RPC (including tools/call) the moment the peer's
// ErrClientClosing/ErrServerClosing surfaces internally (mcp/transport.go:271-284:
// "connection closed by client/server" IS this exact condition, already
// classified by the SDK itself). Using the SDK's own classification is both more
// robust than reconstructing the wire codes by hand and the shape RESEARCH's
// documentation-first rule prefers: matching what the SDK already decided,
// not re-deriving it.

// MountedServer holds a *sdkmcp.ClientSession directly — a concrete type, not an
// interface (D-103/RESEARCH Open Question #1, resolved by the operator): the
// bridge.go Server interface this replaces is dropped entirely, and there is no
// hand-written test double anywhere in this package. sdkmcp.NewInMemoryTransports
// yields a REAL *ClientSession over a net.Pipe(), so daemon-free tests bind a real
// client to a real in-process server.
//
// Fields below the mutex line are guarded by mu; deadErr in particular is written
// only by watch() and must be read only through currentSession()/isDead() — never
// bare, because this package runs -race with goleak and a bare read races the
// write side.
type MountedServer struct {
	name string
	open openSessionFunc
	// identityPool is non-nil only for OAuth-protected HTTP servers. The parent
	// remains the single registry target while calls route to subject-bound children.
	identityPool *identitySessionPool

	mu                sync.Mutex
	session           *sdkmcp.ClientSession
	closed            bool
	deadErr           error // mutex-guarded, written only by watch()
	toolSetDriftErr   error // sticky: once set, never cleared
	acceptedToolNames map[string]struct{}
	bridged           map[string]*bridgedTool
	refreshHook       func()
	processCtx        context.Context

	redialing         bool
	redialDone        chan struct{}
	lastRedialSession *sdkmcp.ClientSession
	lastRedialErr     error
	redialFailures    int
	breakerOpenUntil  time.Time
	nextRedialAfter   time.Time
}

// NewMountedServer constructs a MountedServer with no session attached yet.
// Two-step construction (NewMountedServer then Attach) is required, not
// stylistic: SessionOptions.ToolListChanged must be s.onToolListChanged, which
// needs s to exist before the first session does. mount.go's open closure
// captures the *MountedServer variable itself, not a value, so by the time the
// closure actually runs (the caller invokes it explicitly for the first connect,
// and MountedServer invokes it again on redial) the variable is fully assigned.
func NewMountedServer(name string, open openSessionFunc) *MountedServer {
	return &MountedServer{
		name:              name,
		open:              open,
		bridged:           map[string]*bridgedTool{},
		acceptedToolNames: map[string]struct{}{},
		processCtx:        context.Background(),
	}
}

// Attach records the first live session and starts its death-watch goroutine.
func (s *MountedServer) Attach(session *sdkmcp.ClientSession) {
	s.mu.Lock()
	s.session = session
	s.mu.Unlock()
	go s.watch(session)
}

// setProcessContext records the process-lifetime context a redial must use to
// reopen the replacement transport (Pitfall #2): a redial must never spawn a
// subprocess whose lifetime is tied to the short, deferred-cancel redial-
// handshake timeout.
func (s *MountedServer) setProcessContext(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.processCtx = ctx
}

func (s *MountedServer) processContext() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.processCtx
}

// watch blocks on session.Wait() — the SDK's own push death signal — and records
// the death under the SAME lock that guards the closed-check and the current-
// session identity check, so a redial completing concurrently can never have its
// fresh session marked dead by the outgoing session's own watcher.
//
// Wait() fires promptly for CommandTransport (the stdio child's pipes close) and
// may never fire for a streamable-HTTP peer negotiating protocol >= 2026-07-28,
// because that protocol holds no persistent connection to lose (go-sdk@v1.7.0
// mcp/streamable.go:2095-2098): for HTTP, recovery is per-call — every call is a
// fresh POST — and needs no signal at all. No ticker covers the HTTP case because
// there is nothing to cover.
func (s *MountedServer) watch(session *sdkmcp.ClientSession) {
	err := session.Wait()
	s.mu.Lock()
	dead := !s.closed && s.session == session
	if dead {
		if err == nil {
			// A clean close (Wait's own io.EOF-cleaned read/write errors) is still a
			// dead session from the host's point of view — synthesize a message
			// rather than wrapping a nil error, which fmt's %w renders literally as
			// "%!w(<nil>)".
			err = errors.New("session closed")
		}
		s.deadErr = mcp.TransportErrorf(s.name, err)
	}
	s.mu.Unlock()
	if dead {
		slog.Warn("mcp session closed", "server", s.name, "error", err)
	}
}

// isDead reports whether the current session has been marked dead by watch(),
// under the same lock that guards deadErr — the only sanctioned bare read.
// isSessionTransportFailure is deliberately receiver-free and state-free and must
// never consult deadErr itself.
func (s *MountedServer) isDead() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deadErr != nil
}

// currentSession returns, in priority order: the sticky tool-set-drift error, a
// closed error, the watch()-recorded death, or the live session. Used by
// ListTools and onToolListChanged, for which a dead session is simply a failure
// to propagate — CallToolText uses sessionOrDeath instead, because for IT a dead
// session is not terminal: it is exactly the case that must attempt a redial.
func (s *MountedServer) currentSession() (*sdkmcp.ClientSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.toolSetDriftErr != nil {
		return nil, s.toolSetDriftErr
	}
	if s.closed {
		return nil, fmt.Errorf("%w: mcp %q is closed", mcp.ErrTransport, s.name)
	}
	if s.deadErr != nil {
		return nil, s.deadErr
	}
	if s.session == nil {
		return nil, fmt.Errorf("%w: mcp %q has no session", mcp.ErrTransport, s.name)
	}
	return s.session, nil
}

// sessionOrDeath is CallToolText's own session lookup: terminalErr is non-nil
// only for the two conditions redial can never fix (sticky tool-set drift, an
// explicitly Close()d server) — dead is true when watch() has already recorded
// the session's death, in which case session is still the (dead) session so the
// redial path's single-flight compare-and-swap (`s.session != failed`) still has
// the right identity to compare against.
//
// This split exists because currentSession() alone raced CallToolText: if
// watch() recorded the death BEFORE CallToolText's first lock acquisition, an
// early "return the error unchanged" made a known-dead session behave as if it
// were terminal — silently skipping the no-replay/redial policy entirely,
// exactly opposite of what a dead session should trigger.
func (s *MountedServer) sessionOrDeath() (session *sdkmcp.ClientSession, terminalErr error, dead bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.toolSetDriftErr != nil {
		return nil, s.toolSetDriftErr, false
	}
	if s.closed {
		return nil, fmt.Errorf("%w: mcp %q is closed", mcp.ErrTransport, s.name), false
	}
	if s.deadErr != nil {
		return s.session, nil, true
	}
	if s.session == nil {
		return nil, fmt.Errorf("%w: mcp %q has no session", mcp.ErrTransport, s.name), false
	}
	return s.session, nil, false
}

// deathCause returns the death watch() recorded, or nil if the session is not
// (yet) known dead — used only to render a no-replay error's cause text when
// CallToolText never attempted the call at all because sessionOrDeath already
// reported it dead.
func (s *MountedServer) deathCause() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deadErr
}

// drainTools drains session.Tools' paginated iterator into a slice.
// drainTools is bounded for the same reason the connect is (internal/mcp/bounded_call.go):
// a server that completes its handshake and then stops answering would otherwise hold the
// mount past its deadline. Measured 2026-08-16: the hung-server mount guard
// (TestBuildRegistryWithMCP_HungServerDroppedHealthySurvives) failed here, not at connect —
// the session opened, and the paginated tools/list never observed the 1s handshake ctx.
func drainTools(ctx context.Context, session *sdkmcp.ClientSession) ([]*sdkmcp.Tool, error) {
	return mcp.BoundedCall(ctx, func(ctx context.Context) ([]*sdkmcp.Tool, error) {
		var out []*sdkmcp.Tool
		for t, err := range session.Tools(ctx, nil) {
			if err != nil {
				return nil, err
			}
			out = append(out, t)
		}
		return out, nil
	}, nil)
}

// ListTools drains the current session's paginated tool list.
func (s *MountedServer) ListTools(ctx context.Context) ([]*sdkmcp.Tool, error) {
	if s.identityPool != nil {
		child, err := s.identityPool.server(ctx)
		if err != nil {
			return nil, err
		}
		return child.ListTools(ctx)
	}
	session, err := s.currentSession()
	if err != nil {
		return nil, err
	}
	return drainTools(ctx, session)
}

// decodeResult is the ONE result-decode call site in the tree (RESEARCH Pitfall
// 1): bridgedTool.Execute, the two cmd/aura host-memory callers and the
// readiness check all route through CallTool, so the domain-outcome chain
// cannot be bypassed by adding a caller.
func (s *MountedServer) decodeResult(name string, res *sdkmcp.CallToolResult) (mcp.ToolPayload, error) {
	payload, isErr := mcp.DecodeToolPayload(res)
	if isErr {
		return mcp.ToolPayload{}, mcp.DecodeToolCallError(s.name, name, payload.Text)
	}
	return payload, nil
}

// CallToolText is the text-only projection of CallTool, which is what every
// caller wants that has no view to feed.
func (s *MountedServer) CallToolText(ctx context.Context, name string, args map[string]any) (string, error) {
	payload, err := s.CallTool(ctx, name, args)
	return payload.Text, err
}

// CallTool calls name and decodes its result. A transport-classified failure
// — the session is already known dead (sessionOrDeath's dead==true), or the
// immediate call error is transport-shaped — triggers the redial policy below; a
// plain tool-level error (including a domain isError:true, which never reaches
// here as a Go error at all) is returned unchanged.
func (s *MountedServer) CallTool(ctx context.Context, name string, args map[string]any) (mcp.ToolPayload, error) {
	if s.identityPool != nil {
		child, err := s.identityPool.server(ctx)
		if err != nil {
			return mcp.ToolPayload{}, err
		}
		return child.CallTool(ctx, name, args)
	}
	session, terminalErr, dead := s.sessionOrDeath()
	if terminalErr != nil {
		return mcp.ToolPayload{}, terminalErr
	}

	var callErr error
	if !dead {
		var res *sdkmcp.CallToolResult
		res, callErr = session.CallTool(ctx, &sdkmcp.CallToolParams{Name: name, Arguments: args})
		if callErr == nil {
			return s.decodeResult(name, res)
		}
		if !s.isDead() && !isSessionTransportFailure(callErr) {
			return mcp.ToolPayload{}, callErr
		}
	}
	if callErr == nil {
		// dead was already true: no call was ever attempted, so the no-replay
		// error below needs the recorded death as its cause text.
		callErr = s.deathCause()
	}

	// A mutating call whose transport failed after send must never be silently
	// replayed. An idempotency operation scoped ScopeMCPTool is the strongest
	// signal of that and is checked first, independent of the tool's own
	// mutating/read-only classification.
	if operation, ok := idempotency.OperationFromContext(ctx); ok && operation.Key.Scope == idempotency.ScopeMCPTool {
		return mcp.ToolPayload{}, fmt.Errorf("%w: mcp %q mutating call %q transport failed after send; not replayed or reconnected: %v", mcp.ErrTransport, s.name, name, callErr)
	}

	readOnly := s.toolIsReadOnly(name)
	retry, redialErr := s.redialAfterTransport(ctx, session)
	if redialErr != nil {
		return mcp.ToolPayload{}, redialErr
	}
	if !readOnly {
		return mcp.ToolPayload{}, fmt.Errorf("%w: mcp %q call %q transport failed after send; reconnected but not replayed: %v", mcp.ErrTransport, s.name, name, callErr)
	}
	res, callErr := retry.CallTool(ctx, &sdkmcp.CallToolParams{Name: name, Arguments: args})
	if callErr != nil {
		return mcp.ToolPayload{}, callErr
	}
	return s.decodeResult(name, res)
}

// toolIsReadOnly fails closed for unknown/untracked tools. Only an advertised,
// currently read-only descriptor may redial and reissue automatically.
func (s *MountedServer) toolIsReadOnly(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	bt, ok := s.bridged[name]
	return ok && !bt.Spec().Mutating
}

// Close is idempotent: it stops nothing explicitly (watch exits on its own once
// session.Close's Wait() unblocks) and closes the underlying session.
func (s *MountedServer) Close() error {
	if s.identityPool != nil {
		return s.identityPool.Close()
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	session := s.session
	s.mu.Unlock()
	if session == nil {
		return nil
	}
	return session.Close()
}

// trackBridgedTools records the bridgedTool instances Bridge produced over this
// server, so onToolListChanged's in-place refresh and CallToolText's
// toolIsReadOnly gate can find them by their raw (wire) name.
func (s *MountedServer) trackBridgedTools(bridged []tools.Tool) {
	s.mu.Lock()
	for _, t := range bridged {
		if bt, ok := t.(*bridgedTool); ok {
			s.bridged[bt.name] = bt
		}
	}
	s.mu.Unlock()
	if s.identityPool != nil {
		s.identityPool.syncChildren()
	}
}

// trackAcceptedTools records the tool-name set accepted at mount time, the
// baseline validateToolSetLocked compares every subsequent push against.
func (s *MountedServer) trackAcceptedTools(advertised []*sdkmcp.Tool) {
	s.mu.Lock()
	s.acceptedToolNames = sdkToolNameSet(advertised)
	s.mu.Unlock()
	if s.identityPool != nil {
		s.identityPool.syncChildren()
	}
}

func sdkToolNameSet(advertised []*sdkmcp.Tool) map[string]struct{} {
	names := make(map[string]struct{}, len(advertised))
	for _, t := range advertised {
		names[t.Name] = struct{}{}
	}
	return names
}

// validateToolSetLocked reports a drift error when advertised's NAME SET differs
// from the set accepted at mount — same shape and same error text
// bridge_reconnect.go's validateReconnectToolSetLocked produced. D-104: the
// invariant survives because tools.Registry has no Unregister and no lock
// (internal/agent/tools/spec.go:183-213), so a changed tool set can never be
// re-registered into a live run; it can only be reported.
func (s *MountedServer) validateToolSetLocked(advertised []*sdkmcp.Tool) error {
	if len(s.acceptedToolNames) == 0 {
		return nil
	}
	next := sdkToolNameSet(advertised)
	if len(next) != len(advertised) || len(next) != len(s.acceptedToolNames) {
		return fmt.Errorf("%w: mcp %q tool set changed; restart required", mcp.ErrTransport, s.name)
	}
	for name := range s.acceptedToolNames {
		if _, ok := next[name]; !ok {
			return fmt.Errorf("%w: mcp %q tool set changed; restart required", mcp.ErrTransport, s.name)
		}
	}
	return nil
}

// setRefreshHook registers the callback refreshSpecsLocked fires when an
// in-place spec update actually changed something (Mount wires this to
// invalidateToolSearch).
func (s *MountedServer) setRefreshHook(hook func()) {
	s.mu.Lock()
	s.refreshHook = hook
	s.mu.Unlock()
	if s.identityPool != nil {
		s.identityPool.syncChildren()
	}
}

// refreshSpecsLocked updates each already-registered bridgedTool's stored
// tools.Spec in place from advertised — never the registry map, which is what
// keeps this safe against the frozen-registry invariant.
func (s *MountedServer) refreshSpecsLocked(advertised []*sdkmcp.Tool) {
	// The only site holding the whole reconnect listing, so the only place the
	// model-facing count can be recomputed. Every bridgedTool from one mount
	// carries the identical frozen policy (bridgeToolsWithPolicy scores it once),
	// so any tracked tool answers for the mount.
	for _, bt := range s.bridged {
		warnIfDeferralWouldFlip(s.name, bt.policy, advertised)
		break
	}
	changed := false
	for _, t := range advertised {
		if bt, ok := s.bridged[t.Name]; ok {
			bt.refreshSpec(t)
			changed = true
		}
	}
	if changed && s.refreshHook != nil {
		s.refreshHook()
	}
}

// onToolListChanged is the server-pushed drift notification handler
// (SessionOptions.ToolListChanged -> ClientOptions.ToolListChangedHandler,
// go-sdk@v1.7.0 mcp/client.go:178,337). Registering this handler is also what
// makes the SDK open subscriptions/listen at all under protocol >= 2026-07-28
// (mcp/client.go:333-354) — without a ToolListChangedHandler the client never
// subscribes and this notification never arrives.
//
// A name-set difference marks the server sticky-dead (D-104); a same-name-set
// change (description/schema only) refreshes the affected bridgedTool specs in
// place. Re-listing is bounded by its own timeout, deliberately decoupled from
// ctx (the notification-dispatch context), so a slow re-list cannot be starved by
// however that context is scoped.
func (s *MountedServer) onToolListChanged(ctx context.Context, _ *sdkmcp.ToolListChangedRequest) {
	listCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultMCPRedialTimeout)
	defer cancel()
	session, err := s.currentSession()
	if err != nil {
		return
	}
	advertised, err := drainTools(listCtx, session)
	if err != nil {
		slog.Warn("mcp tool list refresh failed", "server", s.name, "error", err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if driftErr := s.validateToolSetLocked(advertised); driftErr != nil {
		s.toolSetDriftErr = driftErr
		slog.Warn("mcp tool set drift detected", "server", s.name, "error", driftErr)
		return
	}
	s.refreshSpecsLocked(advertised)
}
