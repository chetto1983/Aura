package remotetunnel

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/chetto1983/aura/internal/cloudflareapi"
)

func TestReasonOf(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want Reason
	}{
		{"hostname taken", ErrHostnameTaken, ReasonHostnameTaken},
		{"domain elsewhere", ErrDomainElsewhere, ReasonDomainElsewhere},
		{"no accounts", ErrNoAccounts, ReasonNoAccounts},
		{"account not visible", ErrAccountNotVisible, ReasonAccountNotVisible},
		{"domain invalid", ErrDomainInvalid, ReasonDomainInvalid},
		{"configuration", ErrConfiguration, ReasonUnknown},
		{"ownership", ErrOwnershipConflict, ReasonNotOwned},
		{"lockout", ErrMemberLockout, ReasonNoAdministrator},
		{"remote present", ErrRemotePresent, ReasonOldAddressPresent},
		{"wrapped sentinel", fmt.Errorf("zone: %w", ErrDomainElsewhere), ReasonDomainElsewhere},
		{"unauthorized", &cloudflareapi.APIError{Status: 401}, ReasonTokenRejected},
		{"forbidden", &cloudflareapi.APIError{Status: 403}, ReasonTokenRejected},
		{"retryable", &cloudflareapi.APIError{Retryable: true}, ReasonCloudflareUnreachable},
		{"deadline", context.DeadlineExceeded, ReasonCloudflareUnreachable},
		{"net timeout", timeoutErr{}, ReasonCloudflareUnreachable},
		{"other", errors.New("x"), ReasonUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ReasonOf(tc.err); got != tc.want {
				t.Fatalf("ReasonOf=%q want %q", got, tc.want)
			}
		})
	}
}

func TestFailureWritesTheMappedReason(t *testing.T) {
	h := newHarness(t)
	h.cloud.failure = &cloudflareapi.APIError{Status: 403}
	if h.r.Reconcile(t.Context()) == nil {
		t.Fatal("expected failure")
	}
	if h.store.state.Reason != ReasonTokenRejected || h.store.state.Phase != PhaseError {
		t.Fatalf("phase=%s reason=%q", h.store.state.Phase, h.store.state.Reason)
	}
}

func TestReasonOfAnInactiveToken(t *testing.T) {
	for _, err := range []error{cloudflareapi.ErrTokenInactive, fmt.Errorf("verify: %w", cloudflareapi.ErrTokenInactive)} {
		if got := ReasonOf(err); got != ReasonTokenRejected {
			t.Fatalf("%v: reason=%q", err, got)
		}
		if transient(err) {
			t.Fatalf("%v: a dead token is not worth retrying", err)
		}
	}
}
