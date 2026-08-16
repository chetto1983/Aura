package mcptools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/mcp"
)

// bridge_supervisor_redial.go holds MountedServer's redial machinery — the
// single-flight, breaker/backoff-bounded policy split out of bridge_supervisor.go
// at the ≤600 LOC refactor-on-touch threshold (CLAUDE.md NO GOD CLASS). It is
// still the same file's subject matter, just its own concern: session ownership
// and death-detection stay in bridge_supervisor.go, "what to do about a dead
// session" lives here.

// redialAfterTransport is the single-flight, breaker/backoff-bounded redial:
// concurrent callers observing the SAME failed session collapse onto one dial
// attempt. Ported from bridge_reconnect.go's reconnectAfterTransport, unchanged
// in policy; it no longer re-lists tools on success (drift is now server-pushed
// via onToolListChanged, not something a redial must verify).
func (s *MountedServer) redialAfterTransport(ctx context.Context, failed *sdkmcp.ClientSession) (*sdkmcp.ClientSession, error) {
	for {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return nil, fmt.Errorf("%w: mcp %q is closed", mcp.ErrTransport, s.name)
		}
		if s.session != failed {
			retry := s.session
			s.mu.Unlock()
			if retry == nil {
				return nil, fmt.Errorf("%w: mcp %q has no session", mcp.ErrTransport, s.name)
			}
			return retry, nil
		}
		now := time.Now()
		if err := s.redialBreakerErrorLocked(now); err != nil {
			s.mu.Unlock()
			return nil, err
		}
		if s.redialing {
			done := s.redialDone
			s.mu.Unlock()
			return s.waitForRedial(ctx, done)
		}
		if delay := time.Until(s.nextRedialAfter); delay > 0 {
			s.mu.Unlock()
			if err := sleepContext(ctx, delay); err != nil {
				return nil, err
			}
			continue
		}

		done := make(chan struct{})
		s.redialing = true
		s.redialDone = done
		s.mu.Unlock()

		next, err := s.openReplacement(ctx)

		var closeFailed bool
		var closeNext *sdkmcp.ClientSession
		s.mu.Lock()
		switch {
		case s.closed:
			if next != nil {
				closeNext = next
			}
			next = nil
			err = fmt.Errorf("%w: mcp %q is closed", mcp.ErrTransport, s.name)
		case err == nil:
			s.session = next
			s.deadErr = nil
			s.redialFailures = 0
			s.breakerOpenUntil = time.Time{}
			s.nextRedialAfter = time.Time{}
			closeFailed = failed != nil && failed != next
			go s.watch(next)
		}
		if err != nil && !s.closed {
			s.recordRedialFailureLocked(time.Now(), err)
		}
		s.lastRedialSession = next
		s.lastRedialErr = err
		s.redialing = false
		close(done)
		s.mu.Unlock()

		if closeFailed {
			_ = failed.Close()
		}
		if closeNext != nil {
			_ = closeNext.Close()
		}
		return next, err
	}
}

// openReplacement opens a replacement session. handshakeCtx (derived from parent,
// bounded by timeout, deferred-canceled at return) bounds ONLY the open — the
// process-lifetime context (s.processContext()) is NOT canceled when this
// function returns, so the just-opened replacement survives past openReplacement
// itself (Pitfall #2).
func (s *MountedServer) openReplacement(parent context.Context) (*sdkmcp.ClientSession, error) {
	handshakeCtx, cancel := context.WithTimeout(context.WithoutCancel(parent), defaultMCPRedialTimeout)
	defer cancel()
	next, err := s.open(s.processContext(), handshakeCtx, mcp.SessionOptions{})
	if err != nil {
		return nil, fmt.Errorf("redial mcp %q: %w", s.name, err)
	}
	return next, nil
}

func (s *MountedServer) waitForRedial(ctx context.Context, done <-chan struct{}) (*sdkmcp.ClientSession, error) {
	select {
	case <-done:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.lastRedialSession, s.lastRedialErr
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *MountedServer) redialBreakerErrorLocked(now time.Time) error {
	if s.breakerOpenUntil.IsZero() || !now.Before(s.breakerOpenUntil) {
		return nil
	}
	return fmt.Errorf("%w: mcp %q reconnect breaker open until %s", mcp.ErrTransport, s.name, s.breakerOpenUntil.Format(time.RFC3339))
}

func (s *MountedServer) recordRedialFailureLocked(now time.Time, err error) {
	s.redialFailures++
	backoff := redialBackoff(s.redialFailures)
	s.nextRedialAfter = now.Add(backoff)
	if s.redialFailures >= mcpRedialBreakerAfter {
		s.breakerOpenUntil = now.Add(mcpRedialCooldown)
	}
	slog.Warn("mcp redial failed",
		"server", s.name,
		"failures", s.redialFailures,
		"backoff", backoff.String(),
		"breaker_open_until", s.breakerOpenUntil.Format(time.RFC3339),
		"error", err,
	)
}

// redialBackoff is capped exponential backoff: mcpRedialBackoffMin doubling per
// failure, capped at mcpRedialBackoffMax. Ported unchanged from
// bridge_reconnect.go's reconnectBackoff.
func redialBackoff(failures int) time.Duration {
	if failures <= 0 {
		return 0
	}
	delay := mcpRedialBackoffMin
	for i := 1; i < failures && delay < mcpRedialBackoffMax; i++ {
		delay *= 2
		if delay > mcpRedialBackoffMax {
			return mcpRedialBackoffMax
		}
	}
	return delay
}

// sleepContext sleeps for delay or returns ctx.Err() if ctx is done first.
// Ported unchanged from bridge_reconnect.go; mount_retry.go's MountWithRetry also
// calls this package-level function for its own backoff.
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

// isSessionTransportFailure classifies an error returned directly from
// session.CallTool/ListTools as transport-shaped: a write that never reached the
// peer, a read that hit a closed pipe/connection, or a local "this end is
// shutting down" rejection. It is deliberately receiver-free and state-free — a
// misclassification here either replays a mutation or fails to retry a transient
// boot race, so it is the one piece of this file worth unit-testing directly
// rather than only through MountedServer's own behaviour.
//
// The deadErr disjunct at the call site is NOT part of this function on purpose:
// deadErr is written by the watch goroutine and read on the call path, so it must
// be consulted only through isDead()/currentSession(), under the same mutex that
// guards it — never read bare inside a classifier that has no receiver to lock.
func isSessionTransportFailure(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, io.ErrClosedPipe) || errors.Is(err, net.ErrClosed) {
		return true
	}
	return errors.Is(err, sdkmcp.ErrConnectionClosed)
}
