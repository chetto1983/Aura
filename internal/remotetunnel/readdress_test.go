package remotetunnel

import (
	"errors"
	"slices"
	"testing"
	"time"

	cf "github.com/chetto1983/aura/internal/cloudflareapi"
)

// liveHarness is an install on example.com whose phone test passed, so a pass leaves it healthy.
func liveHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.assertResources(t)
	h.store.state.AcceptedHostname = "aura.example.com"
	h.assertResources(t)
	if h.store.state.Phase != PhaseHealthy {
		t.Fatalf("fixture is %q, want healthy", h.store.state.Phase)
	}
	return h
}

func (h *harness) changeAddress(edit func(*Desired)) {
	h.t.Helper()
	d := h.store.state.Desired
	edit(&d)
	if _, err := h.r.SaveDesired(h.t.Context(), h.store.state.Generation, d, "admin"); err != nil {
		h.t.Fatal(err)
	}
}

// pastBackoff lets the next Reconcile run instead of waiting out the retry delay.
func (h *harness) pastBackoff() {
	h.r.now = func() time.Time { return time.Now().Add(time.Hour) }
}

func TestAddressChanged(t *testing.T) {
	desired := Desired{AccountID: "A", ZoneName: "a.it", PublicLabel: "aura"}
	owned := State{Desired: desired, OwnedZoneName: "a.it", OwnedHostname: "aura.a.it", Resources: Resources{AccountID: "A", TunnelID: "tunnel"}}
	for name, tc := range map[string]struct {
		edit func(*State)
		want bool
	}{
		"unchanged":                   {func(*State) {}, false},
		"other account":               {func(s *State) { s.Desired.AccountID = "B" }, true},
		"other zone":                  {func(s *State) { s.Desired.ZoneName = "b.it" }, true},
		"other label":                 {func(s *State) { s.Desired.PublicLabel = "casa" }, true},
		"tunnel without public DNS":   {func(s *State) { s.Resources.PublicDNSID = "" }, false},
		"nothing owned, nothing made": {func(s *State) { s.Resources, s.OwnedZoneName, s.OwnedHostname = Resources{}, "", "" }, false},
		"nothing made, other account": {func(s *State) { s.Resources = Resources{AccountID: "A"}; s.Desired.AccountID = "B" }, false},
		"made but never recorded":     {func(s *State) { s.OwnedZoneName, s.OwnedHostname = "", "" }, true},
		"only a zone, same address":   {func(s *State) { s.Resources = Resources{AccountID: "A", ZoneID: "zone"} }, false},
	} {
		t.Run(name, func(t *testing.T) {
			s := owned
			tc.edit(&s)
			if got := addressChanged(s); got != tc.want {
				t.Fatalf("addressChanged=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestDomainChangeTearsDownTheOldSetThenProvisionsTheNew(t *testing.T) {
	h := liveHarness(t)
	oldTunnel := h.cloud.tunnel.Name
	h.cloud.allowDetached = true
	h.cloud.nextZone = cf.Zone{ID: "zone-b", Name: "other.it", Status: "active", Account: cf.Account{ID: "account"}}
	h.changeAddress(func(d *Desired) { d.ZoneName = "other.it" })
	applied, deletes := len(h.projection.applied), h.cloud.deletes
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if first := h.projection.applied[applied]; first.Enabled || h.projection.deletesAtApply[applied] != deletes {
		t.Fatalf("connector not projected off before the first deletion: %+v after %d deletions", first, h.projection.deletesAtApply[applied]-deletes)
	}
	if h.cloud.deletes-deletes != 4 {
		t.Fatalf("deleted %d resources, want the tunnel, app, policy and CNAME", h.cloud.deletes-deletes)
	}
	s := h.store.state
	if h.cloud.tunnel.Name == oldTunnel || s.TunnelName != h.cloud.tunnel.Name {
		t.Fatalf("the old tunnel %q survives", oldTunnel)
	}
	if s.Resources.ZoneID != "zone-b" || s.OwnedZoneName != "other.it" || s.OwnedHostname != "aura.other.it" {
		t.Fatalf("zone=%q owned=%q/%q", s.Resources.ZoneID, s.OwnedZoneName, s.OwnedHostname)
	}
	if d := h.cloud.dns[s.Resources.PublicDNSID]; d.Name != "aura.other.it" {
		t.Fatalf("CNAME %q, want aura.other.it", d.Name)
	}
	if s.AcceptedHostname != "" || s.Phase != PhaseConnecting || s.ObservedHealthy {
		t.Fatalf("accepted=%q phase=%q: a new domain asks for the phone test again", s.AcceptedHostname, s.Phase)
	}
	for _, saved := range h.store.log {
		if saved.AcceptedHostname != "" && saved.Desired.ZoneName == "other.it" {
			t.Fatalf("a write brought %q back", saved.AcceptedHostname)
		}
	}
}

func TestAddressChangeIsNotShortCircuitedOnAHealthyRow(t *testing.T) {
	h := liveHarness(t)
	h.changeAddress(func(d *Desired) { d.PublicLabel = "casa" })
	h.store.state.Phase = PhaseHealthy
	deletes := h.cloud.deletes
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if h.cloud.deletes == deletes || h.store.state.OwnedHostname != "casa.example.com" {
		t.Fatalf("a healthy row with a new label was left alone: owned=%q", h.store.state.OwnedHostname)
	}
	for _, saved := range h.store.log[len(h.store.log)-3:] {
		if saved.Phase == PhaseHealthy {
			t.Fatal("the row showed healthy while its address was being replaced")
		}
	}
}

func TestLabelChangeKeepsThePendingZone(t *testing.T) {
	h := newHarness(t)
	h.cloud.zone.Status = "pending"
	h.assertResources(t)
	if h.store.state.Phase != PhaseWaitingNameservers || h.store.state.OwnedHostname != "aura.example.com" {
		t.Fatalf("fixture phase=%q owned=%q", h.store.state.Phase, h.store.state.OwnedHostname)
	}
	zone := h.store.state.Resources.ZoneID
	h.changeAddress(func(d *Desired) { d.PublicLabel = "casa" })
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	s := h.store.state
	if s.Resources.ZoneID != zone || h.cloud.zoneCreates != 1 || s.Phase != PhaseWaitingNameservers || s.OwnedHostname != "casa.example.com" {
		t.Fatalf("zone=%q (was %q) creates=%d phase=%q owned=%q", s.Resources.ZoneID, zone, h.cloud.zoneCreates, s.Phase, s.OwnedHostname)
	}
}

func TestPendingZoneWithoutDNSIsNotAnAddressChange(t *testing.T) {
	h := newHarness(t)
	h.cloud.zone.Status = "pending"
	h.assertResources(t)
	h.assertResources(t)
	if len(h.credentials.saves) != 0 || len(h.projection.applied) != 0 || h.cloud.deletes != 0 {
		t.Fatalf("a pending zone with the same address was torn down: saves=%d applied=%d deletes=%d", len(h.credentials.saves), len(h.projection.applied), h.cloud.deletes)
	}
}

func TestAccountChangeDeletesInTheOldAccount(t *testing.T) {
	h := liveHarness(t)
	h.cloud.accountIDs = []string{"account", "account-b"}
	h.cloud.allowDetached = true
	h.cloud.nextZone = cf.Zone{ID: "zone-b", Name: "example.com", Status: "active", Account: cf.Account{ID: "account-b"}}
	h.changeAddress(func(d *Desired) { d.AccountID = "account-b" })
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if want := []string{"policy:account", "app:account", "tunnel:account"}; !slices.Equal(h.cloud.deleteAccounts, want) {
		t.Fatalf("deleted in %v, want the old account for %v", h.cloud.deleteAccounts, want)
	}
	for _, made := range []string{"zone:account-b", "otp:account-b", "tunnel:account-b", "app:account-b"} {
		if !slices.Contains(h.cloud.createAccounts, made) {
			t.Fatalf("nothing %q in %v", made, h.cloud.createAccounts)
		}
	}
	s := h.store.state
	if s.Resources.AccountID != "account-b" || s.Resources.ZoneID != "zone-b" || s.Resources.OTPProviderID != "otp-account-b" {
		t.Fatalf("resources %+v are not the new account's", s.Resources)
	}
	if s.AcceptedHostname != "aura.example.com" || s.Phase != PhaseHealthy {
		t.Fatalf("accepted=%q phase=%q: the same hostname keeps its phone test", s.AcceptedHostname, s.Phase)
	}
}

func TestInvisibleAccountIsAccountNotVisibleAndTearsNothingDown(t *testing.T) {
	h := liveHarness(t)
	h.cloud.accountIDs = []string{"account", "elsewhere"}
	h.changeAddress(func(d *Desired) { d.AccountID = "missing" })
	deletes, saves := h.cloud.deletes, len(h.credentials.saves)
	err := h.r.Reconcile(t.Context())
	if !errors.Is(err, ErrAccountNotVisible) || h.store.state.Reason != ReasonAccountNotVisible {
		t.Fatalf("err=%v reason=%q", err, h.store.state.Reason)
	}
	if h.cloud.deletes != deletes || len(h.credentials.saves) != saves {
		t.Fatal("the old address was torn down for an account the token cannot see")
	}
}

func TestTeardownWaitsForTheConnectorToDrain(t *testing.T) {
	h := liveHarness(t)
	h.cloud.refuseConnected = true
	h.changeAddress(func(d *Desired) { d.PublicLabel = "casa" })
	err := h.r.Reconcile(t.Context())
	if !errors.Is(err, errConnectorDraining) || h.store.state.Reason != ReasonCloudflareUnreachable || h.store.state.Phase != PhaseDegraded {
		t.Fatalf("err=%v reason=%q phase=%q", err, h.store.state.Reason, h.store.state.Phase)
	}
	h.cloud.tunnel.Status = "inactive"
	h.pastBackoff()
	if err = h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if s := h.store.state; s.OwnedHostname != "casa.example.com" || s.Phase != PhaseConnecting {
		t.Fatalf("owned=%q phase=%q after the connector drained", s.OwnedHostname, s.Phase)
	}
}

func TestRestartDuringTeardownKeepsTheConnectorOff(t *testing.T) {
	h := liveHarness(t)
	h.cloud.publicDNSRemoveErr = transientError()
	h.changeAddress(func(d *Desired) { d.PublicLabel = "casa" })
	applied := len(h.projection.applied)
	if err := h.r.Reconcile(t.Context()); err == nil {
		t.Fatal("the interrupted teardown reported success")
	}
	if h.credentials.saved != "" {
		t.Fatal("the retiring tunnel's credential is still stored")
	}
	for _, p := range h.projection.applied[applied:] {
		if p.Enabled {
			t.Fatal("the connector was projected on during an interrupted teardown")
		}
	}
	h.cloud.publicDNSRemoveErr = nil
	if err := h.newReconciler().Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	after := h.projection.applied[applied:]
	var on []int
	for i, p := range after {
		if p.Enabled {
			on = append(on, i)
		}
	}
	if len(on) != 1 || on[0] != len(after)-1 {
		t.Fatalf("enabled projections at %v of %d: only the new tunnel's, last, may start", on, len(after))
	}
}

func TestTeardownLeftHalfwayResumes(t *testing.T) {
	h := liveHarness(t)
	h.cloud.publicDNSRemoveErr = transientError()
	h.changeAddress(func(d *Desired) { d.PublicLabel = "casa" })
	err := h.r.Reconcile(t.Context())
	if err == nil || h.store.state.Reason != ReasonCloudflareUnreachable {
		t.Fatalf("err=%v reason=%q", err, h.store.state.Reason)
	}
	h.cloud.publicDNSRemoveErr = nil
	h.pastBackoff()
	if err = h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if s := h.store.state; s.OwnedHostname != "casa.example.com" || s.Phase != PhaseConnecting || h.cloud.dns[s.Resources.PublicDNSID].Name != "casa.example.com" {
		t.Fatalf("not provisioned for the new label: %+v", s)
	}
}

func TestTeardownNeverDeletesAForeignResource(t *testing.T) {
	h := liveHarness(t)
	d := h.cloud.dns["public-dns"]
	d.Comment = "someone-else"
	h.cloud.dns["public-dns"] = d
	h.changeAddress(func(d *Desired) { d.PublicLabel = "casa" })
	deletes := h.cloud.deletes
	err := h.r.Reconcile(t.Context())
	if !errors.Is(err, ErrOwnershipConflict) || h.store.state.Reason != ReasonNotOwned || h.cloud.deletes != deletes {
		t.Fatalf("err=%v reason=%q deletes=%d", err, h.store.state.Reason, h.cloud.deletes-deletes)
	}
}

func TestDeleteStillDetachesZoneAndOTP(t *testing.T) {
	h := liveHarness(t)
	if err := h.r.Delete(t.Context(), "admin"); err != nil {
		t.Fatal(err)
	}
	s := h.store.state
	if s.Phase != PhaseDisabled || s.Resources.ZoneID != "" || s.Resources.OTPProviderID != "" || s.OwnedHostname != "" || s.OwnedZoneName != "" || s.TunnelName != "" {
		t.Fatalf("phase=%q resources=%+v owned=%q/%q", s.Phase, s.Resources, s.OwnedZoneName, s.OwnedHostname)
	}
	if h.cloud.zone.ID == "" || h.cloud.otp.ID == "" {
		t.Fatal("the zone or the OTP provider was deleted")
	}
}
