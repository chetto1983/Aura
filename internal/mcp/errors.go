package mcp

import (
	"errors"
	"fmt"
)

// errors.go holds the transport-error vocabulary shared by every MCP client path.
// It lives apart from client.go because client.go is deleted in plan 45.1-03 while
// these three symbols outlive it: mount_retry.go and internal/agent/mcptools decide
// whether a mount failure is worth retrying by asking IsTransportError.

// ErrTransport marks a broken MCP transport pipe/session. Callers use
// IsTransportError rather than matching opaque OS error strings.
var ErrTransport = errors.New("mcp transport error")

// IsTransportError reports whether err wraps ErrTransport AND is worth another attempt.
//
// A server waiting for a person to authorize it is the exception. The refusal reaches the
// caller through the transport — the OAuth handler rejects `initialize` — so it looks
// transient, and it is not: no amount of retrying performs a consent that only a human at a
// browser can perform. Measured 2026-08-24: a newly installed Slack server sat in this loop
// for 40 attempts, holding the whole daemon's startup and leaving the container unhealthy,
// with the log repeating the same line telling the operator to go and authorize it.
func IsTransportError(err error) bool {
	if errors.Is(err, ErrOAuthAuthorizationRequired) {
		return false
	}
	return errors.Is(err, ErrTransport)
}

// TransportErrorf is the single place an SDK dial/call failure acquires Aura's
// transport classification. The SDK's own errors do not wrap ErrTransport, so
// without this every SDK failure would read as permanent to MountWithRetry and a
// server that was merely slow to boot would never be retried.
func TransportErrorf(name string, err error) error {
	return fmt.Errorf("%w: mcp %q: %w", ErrTransport, name, err)
}
