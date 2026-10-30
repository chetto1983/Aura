package remotetunnel

import (
	"context"
	"errors"
	"net"
	"net/http"
	"syscall"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/cloudflareapi"
)

func acceptedHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := h.r.AcceptExternal(t.Context(), h.store.state.Generation); err != nil {
		t.Fatal(err)
	}
	return h
}

// refusedConnection is the error the real client returns when the network is gone.
func refusedConnection(t *testing.T) error {
	t.Helper()
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
	client := cloudflareapi.New("", "token", &http.Client{Transport: roundTripFail{refused}})
	_, err := client.ListAccounts(t.Context())
	if err == nil {
		t.Fatal("client swallowed the transport failure")
	}
	return err
}

type roundTripFail struct{ err error }

func (f roundTripFail) RoundTrip(*http.Request) (*http.Response, error) { return nil, f.err }

func TestRefusedConnectionDuringAnAcceptedOutageDegradesAndRetries(t *testing.T) {
	h := acceptedHarness(t)
	now := time.Unix(1000, 0)
	h.r.now = func() time.Time { return now }
	h.cloud.failure = refusedConnection(t)
	if h.r.Reconcile(t.Context()) == nil {
		t.Fatal("expected the failed pass to report its error")
	}
	if s := h.store.state; s.Phase != PhaseDegraded || s.Reason != ReasonCloudflareUnreachable || s.AcceptedHostname != "aura.example.com" {
		t.Fatalf("state=%+v", s)
	}
	if h.r.retryAt.IsZero() {
		t.Fatal("no retry scheduled")
	}
	h.cloud.failure = nil
	now = now.Add(time.Minute)
	h.r = h.newReconciler()
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if s := h.store.state; s.Phase != PhaseHealthy || s.Reason != "" {
		t.Fatalf("healed state=%+v", s)
	}
}

func TestOutagePassNeverOverwritesTheRedState(t *testing.T) {
	h := acceptedHarness(t)
	h.cloud.tunnel.Status = "down"
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	h.store.log = nil
	for range 2 {
		if err := h.r.Reconcile(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if len(h.store.log) == 0 {
		t.Fatal("the pass recorded nothing")
	}
	for i, s := range h.store.log {
		if s.Phase != PhaseDegraded || s.Reason != ReasonTunnelDown {
			t.Fatalf("write %d left %s/%q", i, s.Phase, s.Reason)
		}
	}
}

func TestHealthyPassNeverShowsProvisioning(t *testing.T) {
	h := acceptedHarness(t)
	h.store.log = nil
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	for i, s := range h.store.log {
		if s.Phase != PhaseHealthy {
			t.Fatalf("write %d left %s", i, s.Phase)
		}
	}
}

func TestDegradedTunnelStillServesTraffic(t *testing.T) {
	h := acceptedHarness(t)
	h.cloud.tunnel.Status = "degraded"
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if s := h.store.state; s.Phase != PhaseHealthy || s.Reason != "" || !s.ObservedHealthy {
		t.Fatalf("state=%+v", s)
	}
}

func TestAcceptanceOfAnotherAddressDoesNotCount(t *testing.T) {
	h := newHarness(t)
	h.store.state.AcceptedHostname = "old.example.com"
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if s := h.store.state; s.Phase != PhaseConnecting || s.ObservedHealthy {
		t.Fatalf("state=%+v", s)
	}
}

func TestEmptyTunnelTokenIsNotADomainProblem(t *testing.T) {
	h := newHarness(t)
	h.cloud.emptyToken = true
	err := h.r.Reconcile(t.Context())
	if !errors.Is(err, ErrConfiguration) {
		t.Fatalf("err=%v", err)
	}
	if h.store.state.Reason == ReasonDomainInvalid || h.store.state.Reason != ReasonUnknown {
		t.Fatalf("reason=%q", h.store.state.Reason)
	}
}

func TestInvalidDomainWritesDomainInvalid(t *testing.T) {
	h := newHarness(t)
	h.store.state.Desired.ZoneName = "evil/path"
	err := h.r.Reconcile(t.Context())
	if !errors.Is(err, ErrConfiguration) || !errors.Is(err, ErrDomainInvalid) {
		t.Fatalf("err=%v", err)
	}
	if h.store.state.Reason != ReasonDomainInvalid {
		t.Fatalf("reason=%q", h.store.state.Reason)
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestFailedClassifiesEveryErrorKind(t *testing.T) {
	for _, tc := range []struct {
		name      string
		failure   error
		phase     Phase
		reason    Reason
		scheduled bool
	}{
		{"deadline", context.DeadlineExceeded, PhaseDegraded, ReasonCloudflareUnreachable, true},
		{"net timeout", timeoutErr{}, PhaseDegraded, ReasonCloudflareUnreachable, true},
		{"retryable api", &cloudflareapi.APIError{Status: 503, Retryable: true}, PhaseDegraded, ReasonCloudflareUnreachable, true},
		{"rejected token", &cloudflareapi.APIError{Status: 401}, PhaseError, ReasonTokenRejected, false},
		{"unclassified", errors.New("boom"), PhaseError, ReasonUnknown, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.cloud.failure = tc.failure
			if !errors.Is(h.r.Reconcile(t.Context()), tc.failure) {
				t.Fatal("cause lost")
			}
			if s := h.store.state; s.Phase != tc.phase || s.Reason != tc.reason {
				t.Fatalf("state=%s/%q", s.Phase, s.Reason)
			}
			if scheduled := !h.r.retryAt.IsZero(); scheduled != tc.scheduled {
				t.Fatalf("retry scheduled=%v", scheduled)
			}
		})
	}
}

func TestFailedIgnoresCancellation(t *testing.T) {
	past, stop := context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer stop()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		name    string
		ctx     context.Context
		failure error
		want    error
	}{
		{"cancelled context", cancelled, errors.New("boom"), context.Canceled},
		{"parent deadline", past, context.DeadlineExceeded, context.DeadlineExceeded},
		{"cancelled error", context.Background(), context.Canceled, context.Canceled},
		{"stale generation", context.Background(), ErrStaleGeneration, ErrStaleGeneration},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.cloud.failure = tc.failure
			if err := h.r.Reconcile(tc.ctx); !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
			if !h.r.retryAt.IsZero() || h.r.attempts != 0 || h.store.state.Phase == PhaseError || h.store.state.Phase == PhaseDegraded {
				t.Fatalf("cancellation persisted a failure: %s", h.store.state.Phase)
			}
		})
	}
}
