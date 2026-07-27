package agui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// secretBearingError stands in for what a real backend hands back: a transport error whose
// message has swallowed the credentials of the call that failed. This is why provisionFail
// refuses to log err.Error() (T-13-07).
type secretBearingError struct{}

func (secretBearingError) Error() string {
	return `pq: password authentication failed for user "aura_app" (dsn=postgres://aura_app:hunter2@db) ` +
		`while writing recovery answer "il mio primo cane"`
}

// TestProvisionFailCauseNamesTheLayerWithoutLeakingIt pins both halves of the trade the
// classifier exists to make.
//
// Before it, provisionFail dropped the error whole, so a first-run failure logged
// `stage=profile write` and nothing else — diagnosing a real one meant reproducing it with
// external instrumentation, which is exactly what a live onboarding failure cost. The
// classifier restores the WHY without restoring the leak: a fixed vocabulary plus the
// concrete error type, none of it caller-supplied.
func TestProvisionFailCauseNamesTheLayerWithoutLeakingIt(t *testing.T) {
	secret := secretBearingError{}

	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, "unspecified"},
		// A budget that ran out is the single most common real cause and is not a refusal:
		// it must be distinguishable at a glance from a backend that said no.
		{"deadline, wrapped twice", fmt.Errorf("session: %w",
			fmt.Errorf("write: %w", context.DeadlineExceeded)), "deadline exceeded"},
		{"canceled", fmt.Errorf("mcp: %w", context.Canceled), "canceled"},
		// fmt.Errorf wrappers are unwrapped away: "*fmt.wrapError" would name the wrapper,
		// never the layer that actually broke.
		{"wrapped backend error", fmt.Errorf("onboarding memory session: %w",
			fmt.Errorf("dial: %w", secret)), "agui.secretBearingError"},
		{"bare backend error", secret, "agui.secretBearingError"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := provisionFailCause(tt.err)
			if got != tt.want {
				t.Fatalf("provisionFailCause = %q, want %q", got, tt.want)
			}
			// The guarantee that made the original code drop the error: nothing the backend
			// wrote into its message may reach the log, in ANY branch.
			for _, leaked := range []string{"hunter2", "il mio primo cane", "aura_app", "dsn="} {
				if strings.Contains(got, leaked) {
					t.Fatalf("cause %q leaked %q from the error message", got, leaked)
				}
			}
		})
	}
}

// TestProvisionFailStillReturnsTheOpaqueSentinel proves the classifier changed only the LOG:
// the error handed back to the caller (and from there to the HTTP surface) is unchanged —
// the fixed stage label plus the sentinel, never the backend's message.
func TestProvisionFailStillReturnsTheOpaqueSentinel(t *testing.T) {
	err := provisionFail("profile write", secretBearingError{})
	if !errors.Is(err, errProvisioningUnavailable) {
		t.Fatalf("err = %v, want the provisioning-unavailable sentinel", err)
	}
	if !strings.Contains(err.Error(), "profile write") {
		t.Errorf("err = %q, want the stage label", err)
	}
	for _, leaked := range []string{"hunter2", "il mio primo cane", "dsn="} {
		if strings.Contains(err.Error(), leaked) {
			t.Fatalf("returned error leaked %q", leaked)
		}
	}
}
