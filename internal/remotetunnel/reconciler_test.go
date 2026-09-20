package remotetunnel

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func TestReconcileResumesEveryPhase(t *testing.T) {
	for _, phase := range []Phase{PhaseValidating, PhaseWaitingNameservers, PhaseProvisioning, PhaseConnecting, PhaseDegraded, PhaseHealthy} {
		t.Run(string(phase), func(t *testing.T) {
			h := newHarness(t)
			h.store.state.Phase = phase
			if err := h.r.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			first := h.cloud.creates
			if err := h.r.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if h.cloud.creates != first || first != 10 {
				t.Fatalf("creates=%d first=%d", h.cloud.creates, first)
			}
			if h.store.state.Phase != PhaseConnecting {
				t.Fatal("health must require acceptance probe")
			}
			if len(h.projection.applied) != 2 || !h.projection.applied[0].Enabled {
				t.Fatal("missing projection")
			}
		})
	}
}

func TestReconcileTransientFailureAtEveryAPIBoundary(t *testing.T) {
	baseline := newHarness(t)
	baseline.assertResources(t)
	for position := 1; position <= baseline.cloud.calls; position++ {
		t.Run(fmt.Sprint(position), func(t *testing.T) {
			h := newHarness(t)
			h.cloud.failureAt = position
			if err := h.r.Reconcile(t.Context()); err == nil {
				t.Fatal("missed boundary failure")
			}
			if h.store.state.Phase != PhaseDegraded {
				t.Fatal("not retryable")
			}
			h.cloud.failureAt = 0
			h.r = h.newReconciler()
			h.assertResources(t)
			if h.cloud.creates != 10 {
				t.Fatalf("duplicate resource after retry: %d", h.cloud.creates)
			}
		})
	}
}

func TestReconcileAmbiguousPolicyCreationRefusesDuplicate(t *testing.T) {
	h := newHarness(t)
	h.cloud.ambiguousPolicy = true
	if err := h.r.Reconcile(t.Context()); err == nil {
		t.Fatal("expected ambiguous timeout")
	}
	creates := h.cloud.creates
	h.cloud.ambiguousPolicy = false
	h.r = h.newReconciler()
	if err := h.r.Reconcile(t.Context()); !errors.Is(err, ErrOwnershipConflict) {
		t.Fatalf("err=%v", err)
	}
	if h.cloud.creates != creates || h.store.state.Resources.PublicPolicyID != "" || len(h.cloud.dns) != 0 {
		t.Fatal("adopted or duplicated unpersisted resource")
	}
}

func TestReconcileConcurrentCallsCreateOnce(t *testing.T) {
	h := newHarness(t)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if err := h.r.Reconcile(t.Context()); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if h.cloud.creates != 10 {
		t.Fatal("concurrent duplicate creation")
	}
}

type testAcceptance struct {
	ready  bool
	err    error
	before func()
}

func (a testAcceptance) Ready(context.Context, State) (bool, error) {
	if a.before != nil {
		a.before()
	}
	return a.ready, a.err
}

func TestReconcileAcceptanceTimeoutBackoffAndRecovery(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"deadline", fmt.Errorf("acceptance probe: %w", context.DeadlineExceeded)},
		{"transport", fmt.Errorf("acceptance probe: %w", &net.DNSError{Err: "i/o timeout", IsTimeout: true})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			now := time.Unix(1000, 0)
			h.r.now = func() time.Time { return now }
			WithAcceptance(testAcceptance{err: tc.err})(h.r)
			if err := h.r.Reconcile(t.Context()); !errors.Is(err, tc.err) {
				t.Fatalf("probe error lost: %v", err)
			}
			if h.store.state.Phase != PhaseDegraded || h.store.state.ObservedHealthy {
				t.Fatalf("timeout phase=%s healthy=%v", h.store.state.Phase, h.store.state.ObservedHealthy)
			}
			calls := h.cloud.calls
			resources := h.store.state.Resources
			if err := h.r.Reconcile(t.Context()); !errors.Is(err, ErrBackoff) || h.cloud.calls != calls {
				t.Fatalf("timeout ignored backoff: %v", err)
			}
			now = now.Add(time.Second)
			WithAcceptance(testAcceptance{ready: true})(h.r)
			if err := h.r.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if h.store.state.Phase != PhaseHealthy || !h.store.state.ObservedHealthy || h.store.state.LastError != "" {
				t.Fatalf("recovery=%+v", h.store.state)
			}
			if h.cloud.creates != 10 || h.store.state.Resources != resources || !h.r.retryAt.IsZero() {
				t.Fatal("recovery recreated resources or retained backoff")
			}
		})
	}
}

func TestReconcileAcceptanceCancellationNeverSchedulesRetry(t *testing.T) {
	for _, mode := range []string{"parent-canceled", "parent-deadline", "probe-canceled"} {
		t.Run(mode, func(t *testing.T) {
			h := newHarness(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "parent-deadline" {
				var stop context.CancelFunc
				ctx, stop = context.WithDeadline(t.Context(), time.Unix(0, 0))
				defer stop()
			}
			probeErr := fmt.Errorf("probe timeout: %w", context.DeadlineExceeded)
			want := error(context.Canceled)
			if mode == "parent-deadline" {
				want = context.DeadlineExceeded
			}
			if mode == "probe-canceled" {
				probeErr = fmt.Errorf("probe stopped: %w", context.Canceled)
			}
			advances := 0
			WithAcceptance(testAcceptance{err: probeErr, before: func() {
				advances = h.store.advances
				if mode == "parent-canceled" {
					cancel()
				}
			}})(h.r)
			if err := h.r.Reconcile(ctx); !errors.Is(err, want) {
				t.Fatalf("cancellation error=%v want=%v", err, want)
			}
			if h.store.advances != advances || !h.r.retryAt.IsZero() || h.r.attempts != 0 {
				t.Fatal("cancellation persisted failure or scheduled retry")
			}
		})
	}
}

func TestReconcileHealthRequiresBothTunnelAndAcceptance(t *testing.T) {
	for _, ready := range []bool{true, false} {
		h := newHarness(t)
		WithAcceptance(testAcceptance{ready: ready})(h.r)
		h.assertResources(t)
		state, err := h.r.Status(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if state.ObservedHealthy != ready || (state.Phase == PhaseHealthy) != ready {
			t.Fatal("health not gated")
		}
		h.cloud.tunnel.Status = "down"
		h.assertResources(t)
		if h.store.state.ObservedHealthy || h.store.state.Phase != PhaseConnecting {
			t.Fatal("disconnected tunnel healthy")
		}
	}
}

func TestReconcileCredentialFailureNeverProjects(t *testing.T) {
	h := newHarness(t)
	h.credentials.err = errors.New("secret-save failure")
	if err := h.r.Reconcile(t.Context()); err == nil {
		t.Fatal("missing save failure")
	}
	if len(h.projection.applied) != 0 {
		t.Fatal("projected unpersisted token")
	}
}

func TestReconcilePersistsEveryCreateAndResumes(t *testing.T) {
	for stop := 1; stop <= 10; stop++ {
		t.Run(string(rune('A'+stop)), func(t *testing.T) {
			h := newHarness(t)
			h.cloud.stopAfter = stop
			if err := h.r.Reconcile(t.Context()); err == nil {
				t.Fatal("expected injected interruption")
			}
			h.cloud.stopAfter = 0
			h.r = h.newReconciler()
			if err := h.r.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if h.cloud.creates != 10 {
				t.Fatalf("duplicate creation: %d", h.cloud.creates)
			}
		})
	}
}

func TestReconcileWaitsForNameservers(t *testing.T) {
	h := newHarness(t)
	h.cloud.zone.Status = "pending"
	for range 2 {
		if err := h.r.Reconcile(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if h.cloud.creates != 1 || h.store.state.Phase != PhaseWaitingNameservers || len(h.projection.applied) != 0 {
		t.Fatal("pending zone was published or recreated")
	}
}

func TestReconcileBackoffAndTerminalFailures(t *testing.T) {
	h := newHarness(t)
	h.cloud.failure = transientError()
	now := time.Unix(1000, 0)
	h.r.now = func() time.Time { return now }
	if h.r.Reconcile(t.Context()) == nil || h.store.state.Phase != PhaseDegraded {
		t.Fatal("transient failure not degraded")
	}
	calls := h.cloud.calls
	if err := h.r.Reconcile(t.Context()); !errors.Is(err, ErrBackoff) || h.cloud.calls != calls {
		t.Fatal("retry ignored backoff")
	}
	now = now.Add(time.Second)
	h.cloud.failure = ErrOwnershipConflict
	if !errors.Is(h.r.Reconcile(t.Context()), ErrOwnershipConflict) || h.store.state.Phase != PhaseError {
		t.Fatal("ownership failure not terminal")
	}
	calls = h.cloud.calls
	if h.r.Reconcile(t.Context()) == nil || h.cloud.calls != calls {
		t.Fatal("automatic terminal retry")
	}
}

func TestDisablePreservesResources(t *testing.T) {
	h := newHarness(t)
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	before := h.store.state.Resources
	calls := h.cloud.calls
	if err := h.r.Disable(context.Background(), "admin"); err != nil {
		t.Fatal(err)
	}
	if before != h.store.state.Resources || h.cloud.calls != calls || h.store.state.Desired.Enabled || h.store.state.Phase != PhaseDisabled {
		t.Fatal("disable mutated remote resources")
	}
	if last := h.projection.applied[len(h.projection.applied)-1]; last.Enabled || last.Token.Reveal() != "" {
		t.Fatal("disable retained projection")
	}
}
