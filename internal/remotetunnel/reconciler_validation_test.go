package remotetunnel

import (
	"errors"
	"strings"
	"testing"
	"time"

	cf "github.com/chetto1983/aura/internal/cloudflareapi"
)

func TestReconcileRejectsInvalidConfigurationBeforeNetwork(t *testing.T) {
	for _, change := range []func(*Desired){
		func(d *Desired) { d.AccountID = "" }, func(d *Desired) { d.ZoneName = "localhost" },
		func(d *Desired) { d.ZoneName = "bad_underscore.com" }, func(d *Desired) { d.ZoneName = strings.Repeat("a", 254) },
		func(d *Desired) { d.PublicLabel = "../bad" }, func(d *Desired) { d.WARPLabel = d.PublicLabel },
		func(d *Desired) {
			d.ZoneName = strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 60)
		},
	} {
		h := newHarness(t)
		change(&h.store.state.Desired)
		if err := h.r.Reconcile(t.Context()); !errors.Is(err, ErrConfiguration) {
			t.Fatalf("err=%v", err)
		}
		if h.cloud.calls != 0 {
			t.Fatal("invalid desired reached network")
		}
	}
	h := newHarness(t)
	h.r.credentials = nil
	if err := h.r.Reconcile(t.Context()); !errors.Is(err, ErrConfiguration) || h.cloud.calls != 0 {
		t.Fatal("unwired credentials published")
	}
}

func TestReconcileStoreFailureDoesNotTouchCloud(t *testing.T) {
	h := newHarness(t)
	h.store.loadError = errors.New("database offline")
	if err := h.r.Reconcile(t.Context()); !errors.Is(err, h.store.loadError) {
		t.Fatalf("err=%v", err)
	}
	if h.cloud.calls != 0 {
		t.Fatal("database failure ignored")
	}
}

func TestReconcileDisabledRestoresIdleProjection(t *testing.T) {
	h := newHarness(t)
	h.store.state.Desired.Enabled = false
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if h.cloud.calls != 0 || len(h.projection.applied) != 1 || h.projection.applied[0].Enabled || h.store.state.Phase != PhaseDisabled {
		t.Fatal("disabled reconciliation published")
	}
}

func TestReconcileBackoffResetAndNewIntent(t *testing.T) {
	h := newHarness(t)
	now := time.Unix(1000, 0)
	h.r.now = func() time.Time { return now }
	h.cloud.failure = transientError()
	for i := range 3 {
		if h.r.Reconcile(t.Context()) == nil {
			t.Fatal("failure expected")
		}
		now = now.Add(time.Second << i)
	}
	h.cloud.failure = nil
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	h.cloud.failure = transientError()
	if h.r.Reconcile(t.Context()) == nil {
		t.Fatal("failure expected")
	}
	if h.r.retryAt.Sub(now) != time.Second {
		t.Fatal("successful reconcile did not reset backoff")
	}
	h.cloud.failure = nil
	if _, err := h.r.SaveDesired(t.Context(), h.store.state.Generation, h.store.state.Desired, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatalf("new intent delayed: %v", err)
	}
}

func TestReconcileRejectsForeignAndUnpersistedResources(t *testing.T) {
	for _, kind := range []string{"account", "zone", "tunnel", "otp", "app", "policy", "posture", "dns", "unknown-tunnel", "unknown-app", "unknown-posture", "unknown-dns"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			h.assertResources(t)
			h.cloud.allowUnpersisted = true
			switch kind {
			case "account":
				h.store.state.Resources.AccountID = "another"
			case "zone":
				h.cloud.zone.Account.ID = "another"
			case "tunnel":
				h.cloud.tunnel.Name = "another"
			case "otp":
				h.cloud.otp.Type = "another"
			case "app":
				a := h.cloud.apps["public-app"]
				a.Name = "another"
				h.cloud.apps["public-app"] = a
			case "policy":
				p := h.cloud.policies["public-policy"]
				p.Name = "another"
				h.cloud.policies["public-policy"] = p
			case "posture":
				h.cloud.posture.Type = "warp"
			case "dns":
				d := h.cloud.dns["public-dns"]
				d.Comment = "another"
				h.cloud.dns["public-dns"] = d
			case "unknown-tunnel":
				h.store.state.Resources.TunnelID = ""
			case "unknown-app":
				h.store.state.Resources.PublicAppID = ""
				h.store.state.Resources.PublicPolicyID = ""
			case "unknown-posture":
				h.store.state.Resources.GatewayPostureID = ""
			case "unknown-dns":
				h.store.state.Resources.PublicDNSID = ""
			}
			before := h.cloud.creates
			if err := h.r.Reconcile(t.Context()); !errors.Is(err, ErrOwnershipConflict) {
				t.Fatalf("err=%v", err)
			}
			if h.cloud.creates != before || len(h.projection.applied) != 1 {
				t.Fatal("conflict published or created")
			}
		})
	}
}

func TestReconcileRepairsOwnedPolicyAndDNSDrift(t *testing.T) {
	h := newHarness(t)
	h.assertResources(t)
	p := h.cloud.policies["warp-policy"]
	p.Require = nil
	h.cloud.policies["warp-policy"] = p
	d := h.cloud.dns["public-dns"]
	d.Content = "foreign-target"
	d.Proxied = false
	h.cloud.dns["public-dns"] = d
	h.assertResources(t)
	if len(h.cloud.policies["warp-policy"].Require) != 2 || !h.cloud.dns["public-dns"].Proxied || h.cloud.dns["public-dns"].Content != "tunnel.cfargotunnel.com" {
		t.Fatal("owned drift not repaired")
	}
}

func TestReconcileFailureDoesNotRemoveLastHealthyProjection(t *testing.T) {
	h := newHarness(t)
	h.assertResources(t)
	h.cloud.failure = &cf.APIError{Status: 429, Retryable: true}
	if err := h.r.Reconcile(t.Context()); err == nil {
		t.Fatal("expected failure")
	}
	if len(h.projection.applied) != 1 || !h.projection.applied[0].Enabled {
		t.Fatal("healthy connector discarded")
	}
}

func TestReconcileRefusesPathSpecificForeignAccessApplication(t *testing.T) {
	h := newHarness(t)
	h.assertResources(t)
	h.cloud.apps["foreign"] = cf.AccessApplication{ID: "foreign", Name: "foreign", Domain: "aura.example.com/admin", Type: "self_hosted"}
	if err := h.r.Reconcile(t.Context()); !errors.Is(err, ErrOwnershipConflict) {
		t.Fatalf("err=%v", err)
	}
}

func TestReconcileStaleGenerationStopsBeforeProjection(t *testing.T) {
	h := newHarness(t)
	h.store.failAdvance = ErrStaleGeneration
	if err := h.r.Reconcile(t.Context()); !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("err=%v", err)
	}
	if h.cloud.creates != 1 || len(h.projection.applied) != 0 {
		t.Fatal("continued after failed ID persistence")
	}
}
