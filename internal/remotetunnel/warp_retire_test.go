package remotetunnel

import (
	"errors"
	"reflect"
	"slices"
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
	if h.cloud.deletesAtPublish-deletes != 4 {
		t.Fatalf("%d WARP deletions came before publish, want all four", h.cloud.deletesAtPublish-deletes)
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

func TestRetireWARPDeniedCNAMEKeepsItsProtection(t *testing.T) {
	for name, deny := range map[string]func(*harness){
		"read":   func(h *harness) { h.cloud.dnsReadErr = denial() },
		"remove": func(h *harness) { h.cloud.dnsRemoveErr = denial() },
	} {
		t.Run(name, func(t *testing.T) {
			h := healthyHarness(t)
			deny(h)
			h.cloud.allowUnpersisted = true // the abandoned records stay in Cloudflare, their IDs forgotten
			if err := h.r.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			owner := h.store.state.TunnelName
			want := []AbandonedResource{{"dns", "aura-warp.example.com"}, {"access_policy", owner + "-warp-members"}, {"access_app", owner + "-warp"}, {"posture", owner + "-gateway"}}
			r := h.store.state.Resources
			if !reflect.DeepEqual(h.store.state.Abandoned, want) || r.WARPDNSID != "" || r.WARPPolicyID != "" || r.WARPAppID != "" || r.GatewayPostureID != "" {
				t.Fatalf("abandoned=%v resources=%+v", h.store.state.Abandoned, r)
			}
			if h.cloud.apps["warp-app"].ID == "" || h.cloud.policies["warp-policy"].ID == "" || h.cloud.dns["warp-dns"].ID == "" {
				t.Fatal("the route lost its protection while it was still published")
			}
			if h.cloud.deletes != 0 || h.cloud.posture.ID == "" {
				t.Fatalf("deletes=%d: a retained route keeps everything it depends on", h.cloud.deletes)
			}
		})
	}
}

func TestRetireWARPDeniedCNAMEThenFailureBeforePublish(t *testing.T) {
	h := healthyHarness(t)
	h.cloud.dnsRemoveErr = denial()
	h.cloud.allowUnpersisted = true
	h.cloud.failPublish = true
	if h.r.Reconcile(t.Context()) == nil {
		t.Fatal("expected the failure before publish")
	}
	if a := h.cloud.apps["warp-app"]; a.ID == "" || a.Domain != "aura-warp.example.com" || h.cloud.dns["warp-dns"].ID == "" {
		t.Fatalf("a published route is open: app=%+v", a)
	}
}

func TestRetireWARPFailureOnADisabledInstallNeverWritesError(t *testing.T) {
	h := newHarness(t)
	h.assertResources(t)
	seedLegacyWARP(h)
	a := h.cloud.apps["warp-app"]
	a.Name = "someone-else"
	h.cloud.apps["warp-app"] = a
	h.store.state.Desired.Enabled = false
	h.store.state.Phase = PhaseDisabled
	h.store.log = nil
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, written := range h.store.log {
		if written.Phase == PhaseError || written.Reason != "" {
			t.Fatalf("a disabled install was written as %s/%q", written.Phase, written.Reason)
		}
	}
	if h.store.state.Phase != PhaseDisabled || h.store.state.Reason != "" || h.store.state.Resources.WARPAppID == "" {
		t.Fatalf("state=%s/%q", h.store.state.Phase, h.store.state.Reason)
	}
}

func TestRetireWARPRetriesOnADisabledInstall(t *testing.T) {
	h := newHarness(t)
	h.assertResources(t)
	seedLegacyWARP(h)
	h.store.state.Desired.Enabled = false
	h.store.state.Phase = PhaseDisabled
	h.store.log = nil
	h.cloud.failure = transientError()
	if err := h.r.Reconcile(t.Context()); err != nil || h.store.state.Resources.WARPAppID == "" {
		t.Fatalf("first pass err=%v", err)
	}
	h.cloud.failure = nil
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	r := h.store.state.Resources
	if r.WARPAppID != "" || r.WARPDNSID != "" || r.GatewayPostureID != "" || h.store.state.Phase != PhaseDisabled || h.store.state.Desired.Enabled {
		t.Fatalf("state=%s %+v", h.store.state.Phase, r)
	}
	for _, written := range h.store.log {
		if written.Desired.Enabled {
			t.Fatal("retirement enabled the install")
		}
	}
}

func TestRetireWARPRetriesOnAnErrorRow(t *testing.T) {
	h := newHarness(t)
	h.assertResources(t)
	seedLegacyWARP(h)
	h.store.state.Phase, h.store.state.Reason = PhaseError, ReasonDomainInvalid
	h.cloud.failure = transientError()
	if err := h.r.Reconcile(t.Context()); !errors.Is(err, ErrTerminal) || h.store.state.Resources.WARPAppID == "" {
		t.Fatalf("first pass err=%v", err)
	}
	h.cloud.failure = nil
	if err := h.r.Reconcile(t.Context()); !errors.Is(err, ErrTerminal) {
		t.Fatalf("second pass err=%v", err)
	}
	if h.store.state.Resources.WARPAppID != "" || h.store.state.Phase != PhaseError || h.store.state.Reason != ReasonDomainInvalid {
		t.Fatalf("state=%s/%q %+v", h.store.state.Phase, h.store.state.Reason, h.store.state.Resources)
	}
}

func TestRetireWARPDeniedPolicyLeavesNoDeadEnd(t *testing.T) {
	for name, deny := range map[string]func(*harness){
		"read":   func(h *harness) { h.cloud.policyReadErr = denial() },
		"remove": func(h *harness) { h.cloud.policyRemoveErr = denial() },
	} {
		t.Run(name, func(t *testing.T) {
			h := healthyHarness(t)
			deny(h)
			h.cloud.allowUnpersisted = true // the abandoned records stay in Cloudflare, their IDs forgotten
			if err := h.r.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			owner := h.store.state.TunnelName
			want := []AbandonedResource{{"access_policy", owner + "-warp-members"}, {"access_app", owner + "-warp"}, {"posture", owner + "-gateway"}}
			r := h.store.state.Resources
			if !reflect.DeepEqual(h.store.state.Abandoned, want) || r.WARPPolicyID != "" || r.WARPAppID != "" || r.GatewayPostureID != "" || r.WARPDNSID != "" {
				t.Fatalf("abandoned=%v resources=%+v", h.store.state.Abandoned, r)
			}
			if h.store.state.Phase != PhaseHealthy || h.cloud.deletes != 1 || h.cloud.dns["warp-dns"].ID != "" {
				t.Fatalf("phase=%s reason=%q deletes=%d", h.store.state.Phase, h.store.state.Reason, h.cloud.deletes)
			}
			if err := h.r.Reconcile(t.Context()); err != nil || h.cloud.deletes != 1 || len(h.store.state.Abandoned) != 3 || h.store.state.Phase != PhaseHealthy {
				t.Fatalf("second pass err=%v deletes=%d abandoned=%v", err, h.cloud.deletes, h.store.state.Abandoned)
			}
		})
	}
}

func TestRetireWARPDeniedAppGoesAlone(t *testing.T) {
	h := healthyHarness(t)
	h.cloud.appRemoveErr = denial()
	h.cloud.allowUnpersisted = true
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := []AbandonedResource{{"access_app", h.store.state.TunnelName + "-warp"}}
	r := h.store.state.Resources
	if !reflect.DeepEqual(h.store.state.Abandoned, want) || r.WARPAppID != "" || r.WARPPolicyID != "" || r.GatewayPostureID != "" || r.WARPDNSID != "" {
		t.Fatalf("abandoned=%v resources=%+v", h.store.state.Abandoned, r)
	}
	if h.cloud.policies["warp-policy"].ID != "" || h.cloud.posture.ID != "" || h.cloud.dns["warp-dns"].ID != "" || h.store.state.Phase != PhaseHealthy {
		t.Fatalf("the policy, posture and CNAME should be retired; phase=%s", h.store.state.Phase)
	}
}

func TestRetireWARPDeniedConfirmReadIsAbandoned(t *testing.T) {
	h := healthyHarness(t)
	h.cloud.postureConfirmErr = denial()
	h.cloud.allowUnpersisted = true
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := []AbandonedResource{{"posture", h.store.state.TunnelName + "-gateway"}}
	if !reflect.DeepEqual(h.store.state.Abandoned, want) || h.store.state.Resources.GatewayPostureID != "" || h.store.state.Phase != PhaseHealthy {
		t.Fatalf("abandoned=%v phase=%s", h.store.state.Abandoned, h.store.state.Phase)
	}
}

func TestRetireWARPForeignPolicyWithADeniedCNAMETouchesNothing(t *testing.T) {
	h := newHarness(t)
	h.assertResources(t)
	seedLegacyWARP(h)
	p := h.cloud.policies["warp-policy"]
	p.Name = "someone-else"
	h.cloud.policies["warp-policy"] = p
	h.cloud.dnsReadErr = denial()
	if err := h.r.Reconcile(t.Context()); !errors.Is(err, ErrOwnershipConflict) {
		t.Fatalf("err=%v", err)
	}
	if h.store.state.Reason != ReasonNotOwned || h.cloud.deletes != 0 || len(h.store.state.Abandoned) != 0 || h.store.state.Resources.WARPDNSID == "" {
		t.Fatalf("reason=%q deletes=%d abandoned=%v", h.store.state.Reason, h.cloud.deletes, h.store.state.Abandoned)
	}
}

func TestRetireWARPBlindTokenWithADeniedCNAMEKeepsEveryID(t *testing.T) {
	h := healthyHarness(t)
	h.cloud.dnsReadErr = denial()
	h.cloud.blind = true
	if err := h.r.Reconcile(t.Context()); err == nil {
		t.Fatal("the pass should report the real problem of the token")
	}
	r := h.store.state.Resources
	if r.WARPDNSID == "" || r.WARPPolicyID == "" || r.WARPAppID == "" || r.GatewayPostureID == "" || len(h.store.state.Abandoned) != 0 || h.cloud.deletes != 0 {
		t.Fatalf("resources=%+v abandoned=%v deletes=%d", r, h.store.state.Abandoned, h.cloud.deletes)
	}
}

func TestRetirementDeletesInTheAccountTheResourcesLiveIn(t *testing.T) {
	h := newHarness(t)
	h.assertResources(t)
	seedLegacyWARP(h)
	h.store.state.Desired.AccountID = "account-b"
	if err := h.r.retireWARP(t.Context(), &h.store.state); err != nil {
		t.Fatal(err)
	}
	if want := []string{"policy:account", "app:account"}; !slices.Equal(h.cloud.deleteAccounts, want) {
		t.Fatalf("deleted in %v, want %v", h.cloud.deleteAccounts, want)
	}
}
