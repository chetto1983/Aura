package remotetunnel

import (
	"errors"
	"net"
	"slices"
	"testing"

	cf "github.com/chetto1983/aura/internal/cloudflareapi"
)

// zoneHeldElsewhere makes Cloudflare answer as it does for a domain active in another account: the
// account asked has no such zone, and the domain's nameservers are Cloudflare's. It returns what
// was asked, as account/zone.
func (h *harness) zoneHeldElsewhere() *[]string {
	asked := &[]string{}
	h.ns.asked = nil
	h.ns.hosts = []string{"jo.ns.cloudflare.com.", "yahir.ns.cloudflare.com."}
	h.cloud.allowDetached = true
	h.cloud.listZones = func(account, name string) ([]cf.Zone, error) {
		*asked = append(*asked, account+"/"+name)
		return nil, nil
	}
	return asked
}

// assertMoveRefused runs the pass and checks it stopped before touching what the old address needs.
func (h *harness) assertMoveRefused(t *testing.T, want error, phase Phase, reason Reason) {
	t.Helper()
	before := h.store.state
	applied, saves, deletes, creates := len(h.projection.applied), len(h.credentials.saves), h.cloud.deletes, h.cloud.creates
	if err := h.r.Reconcile(t.Context()); !errors.Is(err, want) {
		t.Fatalf("err=%v, want %v", err, want)
	}
	s := h.store.state
	if s.Phase != phase || s.Reason != reason {
		t.Fatalf("phase=%q reason=%q, want %q/%q", s.Phase, s.Reason, phase, reason)
	}
	if h.cloud.deletes != deletes || h.cloud.creates != creates {
		t.Fatalf("Cloudflare changed: %d deleted, %d created", h.cloud.deletes-deletes, h.cloud.creates-creates)
	}
	if len(h.projection.applied) != applied || len(h.credentials.saves) != saves || h.credentials.saved == "" {
		t.Fatalf("the connector was touched: %d projections, %d credential saves, stored %t", len(h.projection.applied)-applied, len(h.credentials.saves)-saves, h.credentials.saved != "")
	}
	if s.Resources != before.Resources || s.TunnelName != before.TunnelName || s.OwnedHostname != before.OwnedHostname || s.OwnedZoneName != before.OwnedZoneName {
		t.Fatalf("something was forgotten: %+v owned=%q/%q", s.Resources, s.OwnedZoneName, s.OwnedHostname)
	}
}

func TestAccountChangeToAZoneHeldElsewhereKeepsTheOldAddress(t *testing.T) {
	h := liveHarness(t)
	h.cloud.accountIDs = []string{"account", "account-b"}
	asked := h.zoneHeldElsewhere()
	h.changeAddress(func(d *Desired) { d.AccountID = "account-b" })
	h.assertMoveRefused(t, ErrDomainElsewhere, PhaseError, ReasonDomainElsewhere)
	if want := []string{"account-b/example.com"}; !slices.Equal(*asked, want) {
		t.Fatalf("asked %v, want %v", *asked, want)
	}
}

func TestDomainChangeToAZoneHeldElsewhereKeepsTheOldAddress(t *testing.T) {
	h := liveHarness(t)
	asked := h.zoneHeldElsewhere()
	h.changeAddress(func(d *Desired) { d.ZoneName = "other.it" })
	h.assertMoveRefused(t, ErrDomainElsewhere, PhaseError, ReasonDomainElsewhere)
	if want := []string{"account/other.it"}; !slices.Equal(*asked, want) {
		t.Fatalf("asked %v, want %v", *asked, want)
	}
	if !slices.Equal(h.ns.asked, []string{"other.it"}) {
		t.Fatalf("looked up %v, want the new domain", h.ns.asked)
	}
}

func TestMoveToAnAmbiguousZoneKeepsTheOldAddress(t *testing.T) {
	h := liveHarness(t)
	h.cloud.allowDetached = true
	h.cloud.listZones = func(_, name string) ([]cf.Zone, error) {
		return []cf.Zone{{ID: "one", Name: name}, {ID: "two", Name: name}}, nil
	}
	h.changeAddress(func(d *Desired) { d.ZoneName = "other.it" })
	h.assertMoveRefused(t, ErrOwnershipConflict, PhaseError, ReasonNotOwned)
}

func TestFailingZoneListDeletesNothingAndHeals(t *testing.T) {
	h := liveHarness(t)
	unavailable := transientError()
	h.cloud.allowDetached = true
	h.cloud.listZones = func(string, string) ([]cf.Zone, error) { return nil, unavailable }
	h.changeAddress(func(d *Desired) { d.ZoneName = "other.it" })
	h.assertMoveRefused(t, unavailable, PhaseDegraded, ReasonCloudflareUnreachable)

	h.cloud.listZones = nil
	h.cloud.nextZone = cf.Zone{ID: "zone-b", Name: "other.it", Status: "active", Account: cf.Account{ID: "account"}}
	h.pastBackoff()
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if s := h.store.state; s.OwnedHostname != "aura.other.it" || s.Phase != PhaseConnecting {
		t.Fatalf("owned=%q phase=%q after the list recovered", s.OwnedHostname, s.Phase)
	}
}

// A failing nameserver lookup means "not on Cloudflare", as it does for a first setup.
func TestMoveToADomainNotOnCloudflareTearsDownAndCreatesTheZone(t *testing.T) {
	for name, tc := range map[string]struct {
		hosts []string
		err   error
	}{
		"another provider": {hosts: []string{"dns.register.it."}},
		"nxdomain":         {err: &net.DNSError{IsNotFound: true}},
		"servfail":         {err: &net.DNSError{Err: "server misbehaving", IsTemporary: true}},
		"timeout":          {err: &net.DNSError{Err: "i/o timeout", IsTimeout: true}},
	} {
		t.Run(name, func(t *testing.T) {
			h := liveHarness(t)
			h.ns.hosts, h.ns.err = tc.hosts, tc.err
			h.cloud.allowDetached = true
			h.cloud.zone = cf.Zone{Name: "other.it", Status: "active", Account: cf.Account{ID: "account"}}
			h.changeAddress(func(d *Desired) { d.ZoneName = "other.it" })
			deletes, zones := h.cloud.deletes, h.cloud.zoneCreates
			if err := h.r.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			s := h.store.state
			if h.cloud.deletes-deletes != 4 || h.cloud.zoneCreates-zones != 1 {
				t.Fatalf("deleted %d, created %d zones", h.cloud.deletes-deletes, h.cloud.zoneCreates-zones)
			}
			if s.OwnedHostname != "aura.other.it" || s.Phase != PhaseConnecting {
				t.Fatalf("owned=%q phase=%q", s.OwnedHostname, s.Phase)
			}
		})
	}
}

func TestLabelChangeNeedsNoZoneLookup(t *testing.T) {
	h := liveHarness(t)
	asked := h.zoneHeldElsewhere()
	h.changeAddress(func(d *Desired) { d.PublicLabel = "casa" })
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(*asked) != 0 || len(h.ns.asked) != 0 {
		t.Fatalf("listed %v, looked up %v: the zone stays the one the row holds", *asked, h.ns.asked)
	}
	if s := h.store.state; s.OwnedHostname != "casa.example.com" || s.Phase != PhaseConnecting {
		t.Fatalf("owned=%q phase=%q", s.OwnedHostname, s.Phase)
	}
}

// assertServesWithNoTeardown saves the row's edit and checks the pass kept every resource.
func (h *harness) assertServesWithNoTeardown(t *testing.T, edit func(*Desired)) State {
	t.Helper()
	held := h.store.state.Resources
	deletes, creates := h.cloud.deletes, h.cloud.creates
	h.changeAddress(edit)
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if h.cloud.deletes != deletes || h.cloud.creates != creates || h.credentials.saved == "" {
		t.Fatalf("%d deleted, %d created, credential stored %t", h.cloud.deletes-deletes, h.cloud.creates-creates, h.credentials.saved != "")
	}
	if slices.Contains(h.credentials.saves, cf.Secret("")) {
		t.Fatal("the connector's credential was cleared")
	}
	s := h.store.state
	if s.Resources != held || s.Reason != "" {
		t.Fatalf("resources=%+v reason=%q", s.Resources, s.Reason)
	}
	if last := h.projection.applied[len(h.projection.applied)-1]; !last.Enabled {
		t.Fatalf("the connector is off: %+v", last)
	}
	return s
}

func TestSavingTheOldDomainBackServesAgainAndAsksThePhoneTest(t *testing.T) {
	h := liveHarness(t)
	h.zoneHeldElsewhere()
	h.changeAddress(func(d *Desired) { d.ZoneName = "other.it" })
	h.assertMoveRefused(t, ErrDomainElsewhere, PhaseError, ReasonDomainElsewhere)
	s := h.assertServesWithNoTeardown(t, func(d *Desired) { d.ZoneName = "example.com" })
	if s.Accepted() || s.Phase != PhaseConnecting {
		t.Fatalf("accepted=%q phase=%q: SaveDesired forgot the acceptance at the first save", s.AcceptedHostname, s.Phase)
	}
}

func TestSavingTheOldAccountBackServesAgainWithItsPhoneTest(t *testing.T) {
	h := liveHarness(t)
	h.cloud.accountIDs = []string{"account", "account-b"}
	h.zoneHeldElsewhere()
	h.changeAddress(func(d *Desired) { d.AccountID = "account-b" })
	h.assertMoveRefused(t, ErrDomainElsewhere, PhaseError, ReasonDomainElsewhere)
	s := h.assertServesWithNoTeardown(t, func(d *Desired) { d.AccountID = "account" })
	if !s.Accepted() || s.Phase != PhaseHealthy || !s.ObservedHealthy {
		t.Fatalf("accepted=%q phase=%q: nothing was torn down, so the test still stands", s.AcceptedHostname, s.Phase)
	}
}
