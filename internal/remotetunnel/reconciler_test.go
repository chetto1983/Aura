package remotetunnel

import (
	"context"
	"errors"
	"fmt"
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
			if h.cloud.creates != first || first != 6 {
				t.Fatalf("creates=%d first=%d", h.cloud.creates, first)
			}
			if h.store.state.Phase != PhaseConnecting {
				t.Fatal("health must require acceptance probe")
			}
			if len(h.projection.enabled()) != 2 {
				t.Fatal("missing projection")
			}
		})
	}
}

// A connecting row is the one the operator holds on the phone step, and a pass runs every ten seconds.
func TestConnectingPassNeverWritesProvisioning(t *testing.T) {
	h := newHarness(t)
	h.assertResources(t)
	if s := h.store.state; s.Phase != PhaseConnecting || s.Resources.TunnelID == "" {
		t.Fatalf("fixture phase=%q tunnel=%q", s.Phase, s.Resources.TunnelID)
	}
	h.store.log = nil
	h.assertResources(t)
	if len(h.store.log) == 0 {
		t.Fatal("the pass persisted nothing")
	}
	for i, saved := range h.store.log {
		if saved.Phase != PhaseConnecting {
			t.Fatalf("write %d of %d is %q, want connecting throughout", i+1, len(h.store.log), saved.Phase)
		}
	}
}

func TestConnectingRowThatChangesAddressGoesThroughProvisioning(t *testing.T) {
	h := newHarness(t)
	h.assertResources(t)
	h.changeAddress(func(d *Desired) { d.PublicLabel = "casa" })
	h.store.state.Phase = PhaseConnecting
	h.store.log = nil
	h.assertResources(t)
	if len(h.store.log) == 0 {
		t.Fatal("the pass persisted nothing")
	}
	if first := h.store.log[0].Phase; first != PhaseProvisioning {
		t.Fatalf("the first write is %q: a new address shows the connection step while it is replaced", first)
	}
	if s := h.store.state; s.Phase != PhaseConnecting || s.OwnedHostname != "casa.example.com" {
		t.Fatalf("phase=%q owned=%q", s.Phase, s.OwnedHostname)
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
			if h.cloud.creates != 6 {
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
	if h.cloud.creates != 6 {
		t.Fatal("concurrent duplicate creation")
	}
}

func TestReconcileCredentialFailureNeverProjects(t *testing.T) {
	h := newHarness(t)
	h.credentials.err = errors.New("secret-save failure")
	if err := h.r.Reconcile(t.Context()); err == nil {
		t.Fatal("missing save failure")
	}
	if len(h.projection.enabled()) != 0 {
		t.Fatal("projected unpersisted token")
	}
}

func TestReconcilePersistsEveryCreateAndResumes(t *testing.T) {
	for stop := 1; stop <= 6; stop++ {
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
			if h.cloud.creates != 6 {
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
	if h.cloud.creates != 1 || h.store.state.Phase != PhaseWaitingNameservers || len(h.projection.enabled()) != 0 {
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

func TestRetryNowEndsTheBackoffAndOnlyThePressDoes(t *testing.T) {
	h := newHarness(t)
	h.cloud.failure = transientError()
	now := time.Unix(1000, 0)
	h.r.now = func() time.Time { return now }
	if h.r.Reconcile(t.Context()) == nil {
		t.Fatal("failure expected")
	}
	now = now.Add(time.Second)
	if h.r.Reconcile(t.Context()) == nil {
		t.Fatal("failure expected")
	}
	calls := h.cloud.calls
	if err := h.r.Reconcile(t.Context()); !errors.Is(err, ErrBackoff) || h.cloud.calls != calls {
		t.Fatalf("a pass with no press: err=%v calls %d to %d", err, calls, h.cloud.calls)
	}
	h.r.RetryNow()
	if err := h.r.Reconcile(t.Context()); errors.Is(err, ErrBackoff) || h.cloud.calls == calls {
		t.Fatalf("the press changed nothing: err=%v calls %d to %d", err, calls, h.cloud.calls)
	}
	// Two failures had grown the wait to 2s; the failure after a press starts again from 1s.
	now = now.Add(time.Second)
	calls = h.cloud.calls
	if err := h.r.Reconcile(t.Context()); errors.Is(err, ErrBackoff) || h.cloud.calls == calls {
		t.Fatalf("the press kept the attempts: err=%v calls %d to %d", err, calls, h.cloud.calls)
	}
	calls = h.cloud.calls
	if err := h.r.Reconcile(t.Context()); !errors.Is(err, ErrBackoff) || h.cloud.calls != calls {
		t.Fatalf("automatic retries lost their backoff: err=%v calls %d to %d", err, calls, h.cloud.calls)
	}
}

// waitingHarness is a first setup parked on the nameservers, which can last as long as the
// registrar takes. Cloudflare holds the zone and nothing else.
func waitingHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.cloud.zone.Status = "pending"
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if s := h.store.state; s.Phase != PhaseWaitingNameservers || s.Resources.ZoneID == "" {
		t.Fatalf("not waiting: phase=%q resources=%+v", s.Phase, s.Resources)
	}
	return h
}

func TestDisableWhileWaitingForNameserversParksTheRow(t *testing.T) {
	h := waitingHarness(t)
	before := h.store.state.Resources
	calls := h.cloud.calls
	if err := h.r.Disable(t.Context(), "admin"); err != nil {
		t.Fatal(err)
	}
	// Every action ends with a wake, so the pass after it must leave the row where it is.
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	s := h.store.state
	if s.Phase != PhaseDisabled || s.Desired.Enabled || s.Resources != before || s.Desired.ZoneName != "example.com" || h.cloud.calls != calls {
		t.Fatalf("phase=%q enabled=%v resources=%+v calls %d to %d", s.Phase, s.Desired.Enabled, s.Resources, calls, h.cloud.calls)
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
