package remotetunnel

import (
	"errors"
	"reflect"
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

func denial() error { return &cf.APIError{Status: 403} }

func TestRetireWARPRunsWhileTheInstallIsInError(t *testing.T) {
	h := newHarness(t)
	h.assertResources(t)
	seedLegacyWARP(h)
	h.store.state.Phase, h.store.state.Reason = PhaseError, ReasonDomainInvalid
	if err := h.r.Reconcile(t.Context()); !errors.Is(err, ErrTerminal) {
		t.Fatalf("err=%v", err)
	}
	r := h.store.state.Resources
	if r.WARPAppID != "" || r.WARPDNSID != "" || r.GatewayPostureID != "" || h.cloud.posture.ID != "" {
		t.Fatalf("WARP survives an error row: %+v", r)
	}
	if h.store.state.Phase != PhaseError || h.store.state.Reason != ReasonDomainInvalid {
		t.Fatalf("retirement touched the error: %s/%s", h.store.state.Phase, h.store.state.Reason)
	}
}

func TestRetireWARPRunsOnADisabledInstall(t *testing.T) {
	h := newHarness(t)
	h.assertResources(t)
	seedLegacyWARP(h)
	h.store.state.Desired.Enabled = false
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if h.store.state.Resources.WARPAppID != "" || h.cloud.posture.ID != "" || h.store.state.Desired.Enabled || h.store.state.Phase != PhaseDisabled {
		t.Fatalf("state=%+v", h.store.state)
	}
}

func TestRetireWARPFailureLeavesADisabledInstallAlone(t *testing.T) {
	h := newHarness(t)
	h.assertResources(t)
	seedLegacyWARP(h)
	h.store.state.Desired.Enabled = false
	h.store.state.Phase = PhaseDisabled
	h.cloud.failure = transientError()
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatalf("err=%v", err)
	}
	if h.store.state.Phase != PhaseDisabled || h.store.state.Reason != "" || h.store.state.Resources.WARPAppID == "" || h.store.state.Resources.GatewayPostureID == "" {
		t.Fatalf("state=%s/%q %+v", h.store.state.Phase, h.store.state.Reason, h.store.state.Resources)
	}
}

func TestBackoffSkipsRetirement(t *testing.T) {
	h := newHarness(t)
	h.assertResources(t)
	seedLegacyWARP(h)
	h.cloud.failure = transientError()
	if h.r.Reconcile(t.Context()) == nil {
		t.Fatal("failure expected")
	}
	calls := h.cloud.calls
	if err := h.r.Reconcile(t.Context()); !errors.Is(err, ErrBackoff) || h.cloud.calls != calls {
		t.Fatalf("err=%v calls %d to %d", err, calls, h.cloud.calls)
	}
}

func healthyHarness(t *testing.T) *harness {
	h := newHarness(t)
	h.store.state.AcceptedHostname = "aura.example.com"
	h.assertResources(t)
	seedLegacyWARP(h)
	return h
}

func TestRetireWARPAbandonsWhatTheTokenCannotTouch(t *testing.T) {
	h := healthyHarness(t)
	h.cloud.postureReadErr = denial()
	h.cloud.allowUnpersisted = true // the abandoned posture stays in Cloudflare, its ID forgotten
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := []AbandonedResource{{Kind: "posture", Name: h.store.state.TunnelName + "-gateway"}}
	if !reflect.DeepEqual(h.store.state.Abandoned, want) || h.store.state.Resources.GatewayPostureID != "" {
		t.Fatalf("abandoned=%v posture=%q", h.store.state.Abandoned, h.store.state.Resources.GatewayPostureID)
	}
	if len(h.cloud.apps) != 1 || len(h.cloud.policies) != 1 || h.cloud.dns["warp-dns"].ID != "" || h.store.state.Phase != PhaseHealthy {
		t.Fatalf("phase=%s apps=%d policies=%d", h.store.state.Phase, len(h.cloud.apps), len(h.cloud.policies))
	}
}

func TestRetireWARPAbandonsADeniedDeletion(t *testing.T) {
	h := healthyHarness(t)
	h.cloud.postureRemoveErr = denial()
	h.cloud.allowUnpersisted = true
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := []AbandonedResource{{Kind: "posture", Name: h.store.state.TunnelName + "-gateway"}}
	if !reflect.DeepEqual(h.store.state.Abandoned, want) || h.store.state.Resources.GatewayPostureID != "" || h.store.state.Phase != PhaseHealthy {
		t.Fatalf("abandoned=%v phase=%s", h.store.state.Abandoned, h.store.state.Phase)
	}
}

func TestRetireWARPWithABlindTokenKeepsItsIDs(t *testing.T) {
	h := healthyHarness(t)
	h.cloud.postureReadErr = denial()
	h.cloud.blind = true
	if err := h.r.Reconcile(t.Context()); err == nil {
		t.Fatal("the pass should report the real problem of the token")
	}
	r := h.store.state.Resources
	if r.WARPAppID == "" || r.WARPPolicyID == "" || r.WARPDNSID == "" || r.GatewayPostureID == "" || len(h.store.state.Abandoned) != 0 || h.cloud.deletes != 0 {
		t.Fatalf("resources=%+v abandoned=%v deletes=%d", r, h.store.state.Abandoned, h.cloud.deletes)
	}
}

func TestRetireWARPRefusesAForeignResource(t *testing.T) {
	for name, foreign := range map[string]func(*harness){
		"app": func(h *harness) { a := h.cloud.apps["warp-app"]; a.Name = "someone-else"; h.cloud.apps["warp-app"] = a },
		"policy": func(h *harness) {
			p := h.cloud.policies["warp-policy"]
			p.Name = "someone-else"
			h.cloud.policies["warp-policy"] = p
		},
		"dns comment": func(h *harness) {
			d := h.cloud.dns["warp-dns"]
			d.Comment = "someone-else"
			h.cloud.dns["warp-dns"] = d
		},
		"posture name": func(h *harness) { h.cloud.posture.Name = "someone-else" },
		"posture type": func(h *harness) { h.cloud.posture.Type = "warp" },
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.assertResources(t)
			seedLegacyWARP(h)
			foreign(h)
			if err := h.r.Reconcile(t.Context()); !errors.Is(err, ErrOwnershipConflict) {
				t.Fatalf("err=%v", err)
			}
			if h.store.state.Reason != ReasonNotOwned || h.cloud.deletes != 0 || h.store.state.Resources.WARPDNSID == "" {
				t.Fatalf("reason=%q deletes=%d", h.store.state.Reason, h.cloud.deletes)
			}
		})
	}
}

func TestRetireWARPWithOnlyTheCNAMEGone(t *testing.T) {
	h := newHarness(t)
	h.assertResources(t)
	seedLegacyWARP(h)
	delete(h.cloud.dns, "warp-dns")
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	r := h.store.state.Resources
	if h.cloud.deletes != 3 || r.WARPDNSID != "" || r.WARPAppID != "" || r.WARPPolicyID != "" || r.GatewayPostureID != "" || len(h.cloud.apps) != 1 {
		t.Fatalf("deletes=%d %+v", h.cloud.deletes, r)
	}
}

func TestRetireWARPInterruptedResumes(t *testing.T) {
	h := newHarness(t)
	h.assertResources(t)
	seedLegacyWARP(h)
	h.cloud.failDeleteAfter = 2
	if h.r.Reconcile(t.Context()) == nil {
		t.Fatal("expected interruption")
	}
	r := h.store.state.Resources
	if r.WARPDNSID != "" || r.WARPPolicyID != "" || r.WARPAppID == "" || r.GatewayPostureID == "" {
		t.Fatalf("progress not persisted: %+v", r)
	}
	h.cloud.failDeleteAfter = 0
	h.r = h.newReconciler()
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	r = h.store.state.Resources
	if h.cloud.deletes != 4 || r.WARPAppID != "" || r.GatewayPostureID != "" || r.LegacyWARPHostname != "" {
		t.Fatalf("deletes=%d %+v", h.cloud.deletes, r)
	}
}

func TestForeignDNSRecordOnTheHostnameEndsAsHostnameTaken(t *testing.T) {
	h := newHarness(t)
	h.cloud.dns["foreign"] = cf.DNSRecord{ID: "foreign", Name: "aura.example.com", Type: "CNAME", Comment: "someone-else"}
	if err := h.r.Reconcile(t.Context()); !errors.Is(err, ErrHostnameTaken) || h.store.state.Reason != ReasonHostnameTaken {
		t.Fatalf("err=%v reason=%q", err, h.store.state.Reason)
	}
}
