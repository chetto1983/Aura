package remotetunnel

import (
	"errors"
	"testing"

	cf "github.com/chetto1983/aura/internal/cloudflareapi"
)

// seedLegacyWARP gives a provisioned install the four resources the WARP leg used to create,
// named and commented the way the old reconciler wrote them.
func seedLegacyWARP(h *harness) {
	owner := h.store.state.TunnelName
	h.cloud.apps["warp-app"] = cf.AccessApplication{ID: "warp-app", Name: owner + "-warp", Domain: "aura-warp.example.com", Type: "self_hosted"}
	h.cloud.policies["warp-policy"] = cf.AccessPolicy{ID: "warp-policy", Name: owner + "-warp-members"}
	h.cloud.dns["warp-dns"] = cf.DNSRecord{ID: "warp-dns", Name: "aura-warp.example.com", Type: "CNAME", Comment: owner}
	h.cloud.posture = cf.Posture{ID: "gateway", Name: owner + "-gateway", Type: "gateway"}
	r := &h.store.state.Resources
	r.WARPAppID, r.WARPPolicyID, r.WARPDNSID, r.GatewayPostureID = "warp-app", "warp-policy", "warp-dns", "gateway"
	r.LegacyWARPHostname = "aura-warp.example.com"
}

func TestRetireWARPDeletesOwnedLegacyResources(t *testing.T) {
	h := newHarness(t)
	h.assertResources(t)
	seedLegacyWARP(h)
	deletes := h.cloud.deletes
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	r := h.store.state.Resources
	if r.WARPAppID != "" || r.WARPPolicyID != "" || r.WARPDNSID != "" || r.GatewayPostureID != "" || r.LegacyWARPHostname != "" {
		t.Fatalf("WARP references survive: %+v", r)
	}
	if _, ok := h.cloud.apps["warp-app"]; ok || h.cloud.policies["warp-policy"].ID != "" || h.cloud.dns["warp-dns"].ID != "" || h.cloud.posture.ID != "" {
		t.Fatal("WARP resources survive in Cloudflare")
	}
	if h.cloud.deletes-deletes != 4 {
		t.Fatalf("deletes=%d, want the four WARP resources", h.cloud.deletes-deletes)
	}
	if r.PublicAppID == "" || r.PublicPolicyID == "" || r.PublicDNSID == "" || h.cloud.apps["public-app"].ID == "" || h.cloud.dns["public-dns"].ID == "" {
		t.Fatal("public resources touched")
	}
	if err := h.r.Reconcile(t.Context()); err != nil || h.cloud.deletes-deletes != 4 {
		t.Fatalf("a retired install deletes again: %v", err)
	}
}

func TestRetireWARPToleratesResourcesAlreadyGone(t *testing.T) {
	h := newHarness(t)
	h.assertResources(t)
	r := &h.store.state.Resources
	r.WARPAppID, r.WARPPolicyID, r.WARPDNSID, r.GatewayPostureID = "warp-app", "warp-policy", "warp-dns", "gateway"
	r.LegacyWARPHostname = "aura-warp.example.com"
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if *r != (Resources{AccountID: "account", ZoneID: "zone", TunnelID: "tunnel", PublicDNSID: "public-dns", OTPProviderID: "otp", PublicAppID: "public-app", PublicPolicyID: "public-policy"}) {
		t.Fatalf("resources=%+v", *r)
	}
	if h.store.state.Phase != PhaseConnecting || h.cloud.deletes != 0 {
		t.Fatalf("phase=%s deletes=%d", h.store.state.Phase, h.cloud.deletes)
	}
}

func TestRetireWARPRefusesAForeignResource(t *testing.T) {
	h := newHarness(t)
	h.assertResources(t)
	seedLegacyWARP(h)
	a := h.cloud.apps["warp-app"]
	a.Name = "someone-else"
	h.cloud.apps["warp-app"] = a
	if err := h.r.Reconcile(t.Context()); !errors.Is(err, ErrOwnershipConflict) {
		t.Fatalf("err=%v", err)
	}
	if h.store.state.Reason != ReasonNotOwned || h.cloud.deletes != 0 || h.store.state.Resources.WARPDNSID == "" {
		t.Fatalf("reason=%q deletes=%d", h.store.state.Reason, h.cloud.deletes)
	}
}

func TestDeleteRetiresWARPToo(t *testing.T) {
	h := newHarness(t)
	h.assertResources(t)
	seedLegacyWARP(h)
	if err := h.r.Delete(t.Context(), "admin"); err != nil {
		t.Fatal(err)
	}
	if h.store.state.Resources != (Resources{AccountID: "account"}) || h.cloud.deletes != 8 || h.cloud.posture.ID != "" {
		t.Fatalf("delete left %+v after %d deletes", h.store.state.Resources, h.cloud.deletes)
	}
}

func TestValidateDesiredNoLongerNeedsAWARPLabel(t *testing.T) {
	if err := validateDesired(Desired{Enabled: true, AccountID: "account", ZoneName: "example.com", PublicLabel: "aura"}); err != nil {
		t.Fatal(err)
	}
}

func TestApplicationSessionIsOneMonth(t *testing.T) {
	h := newHarness(t)
	h.assertResources(t)
	if got := h.cloud.apps["public-app"].SessionDuration; got != "730h" {
		t.Fatalf("session=%q", got)
	}
	a := h.cloud.apps["public-app"]
	a.SessionDuration = "24h"
	h.cloud.apps["public-app"] = a
	h.assertResources(t)
	if got := h.cloud.apps["public-app"].SessionDuration; got != "730h" {
		t.Fatalf("an app that had 24h kept %q", got)
	}
}

func TestHostnameUsedByAnotherAccessAppEndsAsHostnameTaken(t *testing.T) {
	for name, domain := range map[string]string{"exact": "aura.example.com", "path": "aura.example.com/admin", "wildcard": "*.example.com"} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.cloud.apps["foreign"] = cf.AccessApplication{ID: "foreign", Name: "foreign", Domain: domain, Type: "self_hosted"}
			if err := h.r.Reconcile(t.Context()); !errors.Is(err, ErrHostnameTaken) {
				t.Fatalf("err=%v", err)
			}
			if h.store.state.Reason != ReasonHostnameTaken {
				t.Fatalf("reason=%q", h.store.state.Reason)
			}
		})
	}
}
