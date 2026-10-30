package remotetunnel

import (
	"errors"
	"reflect"
	"testing"

	cf "github.com/chetto1983/aura/internal/cloudflareapi"
)

func TestAbandonedOfNamesWhatCloudflareShows(t *testing.T) {
	h := liveHarness(t)
	seedLegacyWARP(h)
	owner := h.store.state.TunnelName
	want := []AbandonedResource{
		{Kind: "tunnel", Name: owner},
		{Kind: "dns", Name: "aura.example.com"},
		{Kind: "access_app", Name: owner + "-public"},
		{Kind: "access_policy", Name: owner + "-public-members"},
		{Kind: "dns", Name: "aura-warp.example.com"},
		{Kind: "access_policy", Name: owner + "-warp-members"},
		{Kind: "access_app", Name: owner + "-warp"},
		{Kind: "posture", Name: owner + "-gateway"},
	}
	if got := AbandonedOf(h.store.state); !reflect.DeepEqual(got, want) {
		t.Fatalf("abandoned=%v\nwant     %v", got, want)
	}
}

func TestAbandonedOfListsOnlyWhatIsThere(t *testing.T) {
	s := State{TunnelName: "aura-1", OwnedHostname: "aura.a.it", Resources: Resources{ZoneID: "zone", OTPProviderID: "otp", TunnelID: "tunnel"}}
	want := []AbandonedResource{{Kind: "tunnel", Name: "aura-1"}}
	if got := AbandonedOf(s); !reflect.DeepEqual(got, want) {
		t.Fatalf("abandoned=%v, want just the tunnel: a zone and the OTP provider are never deleted", got)
	}
	if got := AbandonedOf(State{Resources: Resources{ZoneID: "zone"}}); got == nil || len(got) != 0 {
		t.Fatalf("a zone alone is %#v, want an empty non-nil list", got)
	}
}

func TestPassWithoutATunnelProjectsOffBeforeAnythingElse(t *testing.T) {
	h := newHarness(t)
	h.store.state.Desired.ZoneName = "evil/path"
	err := h.r.Reconcile(t.Context())
	if !errors.Is(err, ErrDomainInvalid) || h.store.state.Reason != ReasonDomainInvalid {
		t.Fatalf("err=%v reason=%q", err, h.store.state.Reason)
	}
	if len(h.projection.applied) != 1 || h.projection.applied[0].Enabled {
		t.Fatalf("the connector of a replaced address was not stopped: %+v", h.projection.applied)
	}
	if h.cloud.calls != 0 {
		t.Fatalf("%d Cloudflare calls before the off projection settled", h.cloud.calls)
	}
}

func TestLostProjectionIsRepairedWhileTheZoneIsPending(t *testing.T) {
	h := newHarness(t)
	h.cloud.zone.Status = "pending"
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if h.store.state.Phase != PhaseWaitingNameservers {
		t.Fatalf("phase=%q", h.store.state.Phase)
	}
	if n := len(h.projection.applied); n == 0 || h.projection.applied[n-1].Enabled {
		t.Fatalf("projection after a pass that waits for nameservers: %+v", h.projection.applied)
	}
}

func TestFailedOffProjectionStopsTheProvisioning(t *testing.T) {
	h := newHarness(t)
	h.projection.err = errors.New("read-only file system")
	err := h.r.Reconcile(t.Context())
	if !errors.Is(err, ErrProjection) {
		t.Fatalf("err=%v", err)
	}
	if h.cloud.calls != 0 || h.cloud.creates != 0 || h.store.state.Resources.ZoneID != "" {
		t.Fatalf("provisioning went on with the old connector alive: calls=%d creates=%d", h.cloud.calls, h.cloud.creates)
	}
}

func TestFailedProjectionIsRetriedNotTerminal(t *testing.T) {
	h := newHarness(t)
	h.projection.err = errors.New("read-only file system")
	if err := h.r.Reconcile(t.Context()); err == nil {
		t.Fatal("a failed write passed")
	}
	if s := h.store.state; s.Phase != PhaseDegraded || s.Reason != ReasonUnknown {
		t.Fatalf("phase=%q reason=%q, want degraded/unknown", s.Phase, s.Reason)
	}
	if h.r.retryAt.IsZero() {
		t.Fatal("no retry scheduled")
	}
	generation := h.store.state.Generation
	h.projection.err = nil
	h.pastBackoff()
	h.cloud.zone.Status = "pending"
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if first := h.projection.applied[0]; first.Enabled || len(h.projection.applied) < 2 {
		t.Fatalf("the retry did not project off: %+v", h.projection.applied)
	}
	if h.store.state.Generation != generation || h.store.state.Phase != PhaseWaitingNameservers {
		t.Fatalf("the retry needed a new save: generation %d -> %d, phase %q", generation, h.store.state.Generation, h.store.state.Phase)
	}
}

func TestProjectionFailureIsNeverTerminalWhateverItsCause(t *testing.T) {
	for name, cause := range map[string]error{
		"plain":     errors.New("read-only file system"),
		"forbidden": &cf.APIError{Status: 403},
	} {
		t.Run(name, func(t *testing.T) {
			err := errors.Join(ErrProjection, cause)
			if ReasonOf(err) != ReasonUnknown || !transient(err) {
				t.Fatalf("reason=%q transient=%v", ReasonOf(err), transient(err))
			}
		})
	}
}
