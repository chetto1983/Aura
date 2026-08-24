package mcp

import (
	"errors"
	"fmt"
	"strings"
)

// errors.go holds the transport-error vocabulary shared by every MCP client path.
// It lives apart from client.go because client.go is deleted in plan 45.1-03 while
// these three symbols outlive it: mount_retry.go and internal/agent/mcptools decide
// whether a mount failure is worth retrying by asking IsTransportError.

// ErrTransport marks a broken MCP transport pipe/session. Callers use
// IsTransportError rather than matching opaque OS error strings.
var ErrTransport = errors.New("mcp transport error")

// sdkNoRegistrationMethod is the SDK's message when a server needs OAuth, has no
// pre-registered client configured, and its authorization server advertises no
// registration endpoint to mint one (go-sdk v1.7.0 auth/authorization_code.go:568).
//
// Matched as a string because the SDK builds it with fmt.Errorf and exports no sentinel to
// compare against. That is fragile in one safe direction only: if the wording changes the
// match stops firing and the mount goes back to being retried, which is what it did before
// — nobody is locked out by a missed match.
const sdkNoRegistrationMethod = "no configured client registration methods are supported"

// IsTransportError reports whether err wraps ErrTransport AND is worth another attempt.
//
// OAuth failures are the exception, and they arrive looking exactly like transport
// failures because that is where they surface: the handler rejects `initialize`. Neither
// of the two is fixed by trying again.
//
// A server waiting for a person to authorize it needs a human at a browser. A server whose
// authorization server offers no way to register a client needs an operator to paste a
// client id — Slack publishes no registration_endpoint, so nothing can be issued
// automatically.
//
// Measured 2026-08-24, twice: each of these sat in the mount loop for 40 attempts, holding
// the whole daemon's startup and leaving the container unhealthy, while the log repeated
// the one line that already said what a person had to go and do.
func IsTransportError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrOAuthAuthorizationRequired) {
		return false
	}
	if strings.Contains(err.Error(), sdkNoRegistrationMethod) {
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
