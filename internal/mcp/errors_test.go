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

// TestRegistrationImpossibleIsNotRetryable covers the second way an OAuth server occupies
// the mount loop forever: it needs a client and the authorization server offers no way to
// mint one. Slack publishes no registration_endpoint, so with no MCP_OAUTH_CLIENT_ID
// configured this is the permanent answer, and it too cost 40 attempts of held startup.
func TestRegistrationImpossibleIsNotRetryable(t *testing.T) {
	err := TransportErrorf("Slack", errors.New(
		"calling \"initialize\": no configured client registration methods are supported by the authorization server"))
	if IsTransportError(err) {
		t.Fatal("an unregisterable OAuth client is being retried as a transient transport failure")
	}
}

func TestIsTransportErrorOnNil(t *testing.T) {
	if IsTransportError(nil) {
		t.Fatal("nil is not a transport error")
	}
}
