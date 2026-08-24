package mcp

import (
	"errors"
	"fmt"
	"testing"
)

// TestAuthorizationRequiredIsNotRetryable pins the 2026-08-24 regression: a server waiting
// for a human to authorize it reaches the caller wrapped as a transport failure, and
// MountWithRetry sat through 40 attempts of it, holding daemon startup and leaving the
// container unhealthy. Retrying cannot perform a consent only a browser can perform.
func TestAuthorizationRequiredIsNotRetryable(t *testing.T) {
	authErr := TransportErrorf("Slack", fmt.Errorf("rejected by transport: %w: Slack", ErrOAuthAuthorizationRequired))
	if IsTransportError(authErr) {
		t.Fatal("an authorization-required refusal is being retried as a transient transport failure")
	}
	// The classification must stay narrow: an ordinary dial failure is still retryable,
	// which is the whole reason TransportErrorf exists.
	if !IsTransportError(TransportErrorf("Slack", errors.New("connection refused"))) {
		t.Fatal("an ordinary transport failure stopped being retryable")
	}
}
