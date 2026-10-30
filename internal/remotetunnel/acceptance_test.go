package remotetunnel

import (
	"errors"
	"testing"
	"time"
)

func TestAcceptExternalRecordsTheHostname(t *testing.T) {
	h := newHarness(t)
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	h.store.state.Reason = ReasonUnknown
	if err := h.r.AcceptExternal(t.Context(), h.store.state.Generation); err != nil {
		t.Fatal(err)
	}
	if h.store.state.AcceptedHostname != "aura.example.com" || h.store.state.Phase != PhaseHealthy || !h.store.state.ObservedHealthy || h.store.state.Reason != "" {
		t.Fatalf("acceptance not persisted: %+v", h.store.state)
	}
	if err := h.r.AcceptExternal(t.Context(), h.store.state.Generation); err != nil {
		t.Fatalf("repeat acceptance: %v", err)
	}
}

func TestOutageAfterAcceptanceDegradesAndHealsWithoutANewTest(t *testing.T) {
	h := newHarness(t)
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := h.r.AcceptExternal(t.Context(), h.store.state.Generation); err != nil {
		t.Fatal(err)
	}
	h.cloud.tunnel.Status = "down"
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if s := h.store.state; s.Phase != PhaseDegraded || s.Reason != ReasonTunnelDown || s.AcceptedHostname != "aura.example.com" || s.ObservedHealthy {
		t.Fatalf("outage state=%+v", s)
	}
	h.cloud.tunnel.Status = "healthy"
	h.r = h.newReconciler()
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if s := h.store.state; s.Phase != PhaseHealthy || s.Reason != "" || !s.ObservedHealthy {
		t.Fatalf("healed state=%+v", s)
	}
}

func TestTokenReplacementKeepsAcceptance(t *testing.T) {
	h := newHarness(t)
	h.assertResources(t)
	h.store.state.AcceptedHostname = "aura.example.com"
	state, err := h.r.SaveDesired(t.Context(), h.store.state.Generation, h.store.state.Desired, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if state.AcceptedHostname != "aura.example.com" {
		t.Fatal("same address lost its acceptance")
	}
	h.cloud.tunnel.Status = "healthy"
	if err = h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if h.store.state.Phase != PhaseHealthy || !h.store.state.ObservedHealthy {
		t.Fatalf("phase=%s", h.store.state.Phase)
	}
}

func TestNoAcceptanceStaysConnecting(t *testing.T) {
	h := newHarness(t)
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if s := h.store.state; s.Phase != PhaseConnecting || s.Reason != "" || s.ObservedHealthy {
		t.Fatalf("state=%+v", s)
	}
}

func TestExternalAcceptanceRejectsStaleDisabledUnownedAndUnhealthy(t *testing.T) {
	for _, mode := range []string{"stale", "disabled", "unowned", "unhealthy", "error", "terminal", "missing", "deleted", "load", "persist"} {
		t.Run(mode, func(t *testing.T) {
			h := newHarness(t)
			if err := h.r.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			generation := h.store.state.Generation
			want := ErrConfiguration
			switch mode {
			case "stale":
				generation--
				want = ErrStaleGeneration
			case "disabled":
				h.store.state.Desired.Enabled = false
			case "unowned":
				h.cloud.tunnel.Name = "foreign"
				want = ErrOwnershipConflict
			case "unhealthy":
				h.store.state.ObservedHealthy = true
				h.store.state.Phase = PhaseHealthy
				h.cloud.tunnel.Status = "down"
			case "error":
				h.cloud.failure = errors.New("unavailable")
				want = h.cloud.failure
			case "terminal":
				h.store.state.Phase = PhaseError
			case "missing":
				h.store.state.Resources.TunnelID = ""
				h.cloud.allowUnpersisted = true
			case "deleted":
				now := time.Now()
				h.cloud.tunnel.DeletedAt = &now
				want = ErrOwnershipConflict
			case "load":
				h.store.loadError = errors.New("load failed")
				want = h.store.loadError
			case "persist":
				h.cloud.tunnel.Status = "down"
				h.store.failAdvance = errors.New("commit failed")
				want = h.store.failAdvance
			}
			if err := h.r.AcceptExternal(t.Context(), generation); !errors.Is(err, want) {
				t.Fatalf("error=%v want=%v", err, want)
			}
			if h.store.state.ObservedHealthy {
				t.Fatal("invalid acceptance persisted")
			}
			if mode == "unhealthy" && h.store.state.Phase != PhaseConnecting {
				t.Fatal("disconnected connector still reported healthy")
			}
		})
	}
}

func TestAcceptExternalAcceptsADegradedTunnel(t *testing.T) {
	h := newHarness(t)
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	h.cloud.tunnel.Status = "degraded"
	if err := h.r.AcceptExternal(t.Context(), h.store.state.Generation); err != nil {
		t.Fatalf("a tunnel serving over a failed connection was refused: %v", err)
	}
	if s := h.store.state; !s.Accepted() || s.Phase != PhaseHealthy || !s.ObservedHealthy {
		t.Fatalf("state=%+v", s)
	}
}

func TestAcceptExternalNeverDemotesAnAcceptedRow(t *testing.T) {
	h := newHarness(t)
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := h.r.AcceptExternal(t.Context(), h.store.state.Generation); err != nil {
		t.Fatal(err)
	}
	h.cloud.tunnel.Status = "down"
	if err := h.r.AcceptExternal(t.Context(), h.store.state.Generation); !errors.Is(err, ErrConfiguration) {
		t.Fatalf("err=%v", err)
	}
	if s := h.store.state; s.Phase != PhaseHealthy || !s.ObservedHealthy || !s.Accepted() {
		t.Fatalf("an outage asked an accepted address for the phone test again: %+v", s)
	}
}

func TestAcceptedNeedsTheCurrentAddress(t *testing.T) {
	desired := Desired{AccountID: "account", ZoneName: "example.com", PublicLabel: "aura"}
	for name, tc := range map[string]struct {
		accepted string
		desired  Desired
		want     bool
	}{
		"same address":       {"aura.example.com", desired, true},
		"another label":      {"aura.example.com", Desired{ZoneName: "example.com", PublicLabel: "home"}, false},
		"another zone":       {"aura.example.com", Desired{ZoneName: "example.it", PublicLabel: "aura"}, false},
		"never accepted":     {"", desired, false},
		"no address at all":  {"", Desired{}, false},
		"stale on empty row": {"aura.example.com", Desired{}, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := (State{Desired: tc.desired, AcceptedHostname: tc.accepted}).Accepted(); got != tc.want {
				t.Fatalf("Accepted=%v want %v", got, tc.want)
			}
		})
	}
}

func TestValidateDesiredFirstStep(t *testing.T) {
	for name, tc := range map[string]struct {
		d    Desired
		want error
	}{
		"account only":                   {Desired{AccountID: "account"}, nil},
		"first step without an account":  {Desired{}, ErrConfiguration},
		"enabled without a domain":       {Desired{Enabled: true, AccountID: "account", PublicLabel: "aura"}, ErrDomainInvalid},
		"disabled with a malformed zone": {Desired{AccountID: "account", ZoneName: "evil/path", PublicLabel: "aura"}, ErrDomainInvalid},
		"complete":                       {Desired{Enabled: true, AccountID: "account", ZoneName: "example.com", PublicLabel: "aura"}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateDesired(tc.d); !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
		})
	}
}
