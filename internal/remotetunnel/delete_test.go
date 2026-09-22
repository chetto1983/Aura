package remotetunnel

import (
	"errors"
	cf "github.com/chetto1983/aura/internal/cloudflareapi"
	"testing"
	"time"
)

func TestDeleteRefusesForeignResource(t *testing.T) {
	h := newHarness(t)
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	h.cloud.tunnel.Name = "someone-else"
	if err := h.r.Delete(t.Context(), "admin"); !errors.Is(err, ErrOwnershipConflict) {
		t.Fatalf("err=%v", err)
	}
	if h.store.state.Resources.TunnelID == "" || h.cloud.deletes != 0 {
		t.Fatal("foreign resource deletion started")
	}
}

func TestDeleteConfirmsOwnedTombstoneAndRefusesForeignTombstone(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		h := newHarness(t)
		h.assertResources(t)
		deleted := time.Now()
		h.cloud.tunnel.DeletedAt = &deleted
		if foreign {
			h.cloud.tunnel.Name = "foreign"
		}
		err := h.r.Delete(t.Context(), "admin")
		if foreign {
			if !errors.Is(err, ErrOwnershipConflict) || h.cloud.deletes != 0 {
				t.Fatal("foreign tombstone authorized deletion")
			}
		} else if err != nil || h.cloud.deletes != 7 || h.store.state.Resources.TunnelID != "" {
			t.Fatalf("tombstone not detached: %v", err)
		}
	}
}

func TestDeleteRetainsIDsUntilAbsenceConfirmed(t *testing.T) {
	h := newHarness(t)
	h.assertResources(t)
	before := h.store.state.Resources
	h.cloud.keepDeleted = true
	if err := h.r.Delete(t.Context(), "admin"); !errors.Is(err, ErrRemotePresent) {
		t.Fatalf("err=%v", err)
	}
	if h.store.state.Resources != before || h.store.state.Phase != PhaseDeleting {
		t.Fatal("cleared IDs before absence")
	}
}

func TestDeleteOwnershipGuardsEveryResource(t *testing.T) {
	for _, resource := range []string{"tunnel", "public-app", "warp-app", "public-policy", "warp-policy", "gateway", "public-dns", "warp-dns", "marker"} {
		t.Run(resource, func(t *testing.T) {
			h := newHarness(t)
			h.assertResources(t)
			switch resource {
			case "tunnel":
				h.cloud.tunnel.Name = "foreign"
			case "public-app", "warp-app":
				a := h.cloud.apps[resource]
				a.Name = "foreign"
				h.cloud.apps[resource] = a
			case "public-policy", "warp-policy":
				p := h.cloud.policies[resource]
				p.Name = "foreign"
				h.cloud.policies[resource] = p
			case "gateway":
				h.cloud.posture.Name = "foreign"
			case "public-dns", "warp-dns":
				d := h.cloud.dns[resource]
				d.Comment = "foreign"
				h.cloud.dns[resource] = d
			case "marker":
				h.store.state.TunnelName = "foreign"
			}
			if err := h.r.Delete(t.Context(), "admin"); !errors.Is(err, ErrOwnershipConflict) {
				t.Fatalf("err=%v", err)
			}
			if h.cloud.deletes != 0 {
				t.Fatal("deleted despite foreign preflight")
			}
		})
	}
}

func TestDeleteResumesAfterRemoteAlreadyAbsent(t *testing.T) {
	h := newHarness(t)
	h.assertResources(t)
	delete(h.cloud.dns, "warp-dns")
	delete(h.cloud.policies, "warp-policy")
	delete(h.cloud.apps, "warp-app")
	h.cloud.posture = cf.Posture{}
	if err := h.r.Delete(t.Context(), "admin"); err != nil {
		t.Fatal(err)
	}
	if h.cloud.deletes != 4 || h.store.state.Phase != PhaseDisabled {
		t.Fatal("absence reconciliation failed")
	}
}

func TestDeleteResumesAndPreservesZoneAndOTP(t *testing.T) {
	h := newHarness(t)
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	h.cloud.failDeleteAfter = 3
	if err := h.r.Delete(t.Context(), "admin"); err == nil {
		t.Fatal("expected interruption")
	}
	h.cloud.failDeleteAfter = 0
	h.r = h.newReconciler()
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if h.cloud.deletes != 8 || h.cloud.zone.ID == "" || h.cloud.otp.ID == "" || h.store.state.Resources != (Resources{AccountID: "account"}) || h.store.state.Phase != PhaseDisabled {
		t.Fatalf("delete incomplete: %+v count=%d", h.store.state, h.cloud.deletes)
	}
}
