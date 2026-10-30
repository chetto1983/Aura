package remotetunnel

import (
	"errors"
	"slices"
	"testing"

	cf "github.com/chetto1983/aura/internal/cloudflareapi"
)

// tunnelHasConnections is what Cloudflare answers when a tunnel still has connections: HTTP 400,
// code 1022 (cloudflare/terraform-provider-cloudflare issue 5255).
func tunnelHasConnections() error { return &cf.APIError{Status: 400, Code: 1022} }

// untouched fails the test when a refused teardown stopped the connector or deleted anything.
func (h *harness) untouched(applied, saves, deletes int) {
	h.t.Helper()
	if len(h.projection.applied) != applied || len(h.credentials.saves) != saves || h.cloud.deletes != deletes {
		h.t.Fatalf("a refused teardown touched the old address: applied=%d saves=%d deletes=%d", len(h.projection.applied)-applied, len(h.credentials.saves)-saves, h.cloud.deletes-deletes)
	}
}

func TestTeardownRetriesATunnelReadAsDownWith1022(t *testing.T) {
	h := liveHarness(t)
	h.cloud.tunnel.Status = "down"
	h.cloud.tunnelDeleteErr = tunnelHasConnections()
	h.changeAddress(func(d *Desired) { d.PublicLabel = "casa" })
	err := h.r.Reconcile(t.Context())
	if err == nil || h.store.state.Reason != ReasonCloudflareUnreachable || h.store.state.Phase != PhaseDegraded {
		t.Fatalf("err=%v reason=%q phase=%q: a tunnel with lingering connections is retried", err, h.store.state.Reason, h.store.state.Phase)
	}
	h.cloud.tunnelDeleteErr = nil
	h.pastBackoff()
	if err = h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if s := h.store.state; s.OwnedHostname != "casa.example.com" || s.Phase != PhaseConnecting {
		t.Fatalf("owned=%q phase=%q after Cloudflare stopped refusing", s.OwnedHostname, s.Phase)
	}
}

func TestTunnelDeletionWithAnotherCodeIsNotRetried(t *testing.T) {
	h := liveHarness(t)
	h.cloud.tunnelDeleteErr = &cf.APIError{Status: 400, Code: 1000}
	h.changeAddress(func(d *Desired) { d.PublicLabel = "casa" })
	if err := h.r.Reconcile(t.Context()); err == nil {
		t.Fatal("a refusal for another reason reported success")
	}
	if s := h.store.state; s.Phase != PhaseError || s.Reason != ReasonUnknown {
		t.Fatalf("phase=%q reason=%q, want a terminal unknown error", s.Phase, s.Reason)
	}
}

func TestBlindTokenKeepsTheOldConnectorServing(t *testing.T) {
	for name, warp := range map[string]bool{"plain": false, "with a WARP leftover": true} {
		t.Run(name, func(t *testing.T) {
			h := liveHarness(t)
			if warp {
				seedLegacyWARP(h)
				h.cloud.dnsReadErr = denial()
			}
			h.cloud.accountIDs = []string{"account-b"}
			h.changeAddress(func(d *Desired) { d.AccountID = "account-b" })
			applied, saves, deletes := len(h.projection.applied), len(h.credentials.saves), h.cloud.deletes
			err := h.r.Reconcile(t.Context())
			if !errors.Is(err, ErrAccountNotVisible) || h.store.state.Reason != ReasonAccountNotVisible {
				t.Fatalf("err=%v reason=%q", err, h.store.state.Reason)
			}
			h.untouched(applied, saves, deletes)
		})
	}
}

func TestBlindTokenBetweenPreflightAndRetirementStopsTheTeardown(t *testing.T) {
	h := liveHarness(t)
	seedLegacyWARP(h)
	h.cloud.dnsRemoveErr = denial()
	h.cloud.listCalls, h.cloud.blindAfterLists = 0, 1
	s := h.store.state
	err := h.r.teardown(t.Context(), &s)
	if !errors.Is(err, ErrAccountNotVisible) {
		t.Fatalf("err=%v, want account_not_visible", err)
	}
	if s.TunnelName == "" || s.Resources.LegacyWARPHostname == "" {
		t.Fatalf("tunnel name %q and WARP hostname %q must outlive a retirement that could not finish", s.TunnelName, s.Resources.LegacyWARPHostname)
	}
}

func TestForeignPublicResourceKeepsTheOldConnectorServing(t *testing.T) {
	h := liveHarness(t)
	d := h.cloud.dns["public-dns"]
	d.Comment = "someone-else"
	h.cloud.dns["public-dns"] = d
	h.changeAddress(func(d *Desired) { d.PublicLabel = "casa" })
	applied, saves, deletes := len(h.projection.applied), len(h.credentials.saves), h.cloud.deletes
	err := h.r.Reconcile(t.Context())
	if !errors.Is(err, ErrOwnershipConflict) || h.store.state.Reason != ReasonNotOwned {
		t.Fatalf("err=%v reason=%q", err, h.store.state.Reason)
	}
	h.untouched(applied, saves, deletes)
}

func TestDeleteWithABlindTokenStopsBeforeTouchingAnything(t *testing.T) {
	h := liveHarness(t)
	h.cloud.accountIDs = []string{"elsewhere"}
	applied, saves, deletes := len(h.projection.applied), len(h.credentials.saves), h.cloud.deletes
	err := h.r.Delete(t.Context(), "admin")
	if !errors.Is(err, ErrAccountNotVisible) || h.store.state.Reason != ReasonAccountNotVisible {
		t.Fatalf("err=%v reason=%q", err, h.store.state.Reason)
	}
	h.untouched(applied, saves, deletes)
}

func TestTeardownClearsTheCredentialBeforeProjectingOff(t *testing.T) {
	h := liveHarness(t)
	h.changeAddress(func(d *Desired) { d.PublicLabel = "casa" })
	before := len(h.events)
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := h.events[before : before+2]; !slices.Equal(got, []string{"save:", "apply:false"}) {
		t.Fatalf("first two steps %v, want the credential cleared and then the connector projected off", got)
	}
}

func TestFailedCredentialClearStopsTheTeardown(t *testing.T) {
	h := liveHarness(t)
	h.changeAddress(func(d *Desired) { d.PublicLabel = "casa" })
	h.credentials.err = errors.New("settings unavailable")
	applied, deletes := len(h.projection.applied), h.cloud.deletes
	if err := h.r.Reconcile(t.Context()); err == nil {
		t.Fatal("a credential that could not be cleared reported success")
	}
	if len(h.projection.applied) != applied || h.cloud.deletes != deletes {
		t.Fatal("the teardown went on past a credential it could not clear")
	}
}

func TestFailedProjectionStopsTheTeardown(t *testing.T) {
	h := liveHarness(t)
	h.changeAddress(func(d *Desired) { d.PublicLabel = "casa" })
	h.projection.err = errors.New("sidecar unavailable")
	deletes := h.cloud.deletes
	if err := h.r.Reconcile(t.Context()); err == nil {
		t.Fatal("a connector that could not be stopped reported success")
	}
	if h.cloud.deletes != deletes {
		t.Fatal("resources were deleted while the connector could still be serving them")
	}
}

func TestTeardownWithoutCredentialsIsAConfigurationError(t *testing.T) {
	h := liveHarness(t)
	h.r.credentials = nil
	applied, deletes := len(h.projection.applied), h.cloud.deletes
	s := h.store.state
	if err := h.r.teardown(t.Context(), &s); !errors.Is(err, ErrConfiguration) {
		t.Fatalf("teardown err=%v", err)
	}
	if err := h.r.Delete(t.Context(), "admin"); !errors.Is(err, ErrConfiguration) {
		t.Fatalf("Delete err=%v", err)
	}
	if len(h.projection.applied) != applied || h.cloud.deletes != deletes {
		t.Fatal("a teardown without a credential port touched the connector or Cloudflare")
	}
}

func TestDeleteAfterAnAccountChangeDeletesInTheOldAccount(t *testing.T) {
	h := liveHarness(t)
	h.cloud.accountIDs = []string{"account", "account-b"}
	h.changeAddress(func(d *Desired) { d.AccountID = "account-b" })
	if err := h.r.Delete(t.Context(), "admin"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"policy:account", "app:account", "tunnel:account"}; !slices.Equal(h.cloud.deleteAccounts, want) {
		t.Fatalf("deleted in %v, want the old account for %v", h.cloud.deleteAccounts, want)
	}
	if h.store.state.Phase != PhaseDisabled {
		t.Fatalf("phase=%q", h.store.state.Phase)
	}
}

func TestDeleteMidTeardownFinishesTheDeletion(t *testing.T) {
	h := liveHarness(t)
	h.cloud.publicDNSRemoveErr = transientError()
	h.changeAddress(func(d *Desired) { d.PublicLabel = "casa" })
	if err := h.r.Reconcile(t.Context()); err == nil {
		t.Fatal("the interrupted teardown reported success")
	}
	h.cloud.publicDNSRemoveErr = nil
	if err := h.r.Delete(t.Context(), "admin"); err != nil {
		t.Fatal(err)
	}
	if s := h.store.state; s.Phase != PhaseDisabled || HasResources(s.Resources) || s.TunnelName != "" {
		t.Fatalf("phase=%q resources=%+v", s.Phase, s.Resources)
	}
}

func TestDisableMidTeardownParksTheRowAndEnablingResumes(t *testing.T) {
	h := liveHarness(t)
	h.cloud.publicDNSRemoveErr = transientError()
	h.changeAddress(func(d *Desired) { d.PublicLabel = "casa" })
	if err := h.r.Reconcile(t.Context()); err == nil {
		t.Fatal("the interrupted teardown reported success")
	}
	h.cloud.publicDNSRemoveErr = nil
	if err := h.r.Disable(t.Context(), "admin"); err != nil {
		t.Fatal(err)
	}
	if last := h.projection.applied[len(h.projection.applied)-1]; last.Enabled || h.store.state.Phase != PhaseDisabled || h.store.state.Resources.TunnelID == "" {
		t.Fatalf("enabled=%v phase=%q tunnel=%q: Disable keeps the resources and the connector off", last.Enabled, h.store.state.Phase, h.store.state.Resources.TunnelID)
	}
	h.changeAddress(func(d *Desired) { d.Enabled = true })
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if s := h.store.state; s.OwnedHostname != "casa.example.com" || s.Phase != PhaseConnecting {
		t.Fatalf("owned=%q phase=%q after enabling again", s.OwnedHostname, s.Phase)
	}
}
