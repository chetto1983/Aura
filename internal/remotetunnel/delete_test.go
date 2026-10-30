package remotetunnel

import (
	"errors"
	"testing"
	"time"
)

func TestDeleteWhileWaitingForNameserversForgetsTheZoneAndKeepsTheAddress(t *testing.T) {
	h := waitingHarness(t)
	if err := h.r.Delete(t.Context(), "admin"); err != nil {
		t.Fatal(err)
	}
	creates := h.cloud.creates
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	s := h.store.state
	if s.Phase != PhaseDisabled || HasResources(s.Resources) || s.Desired.ZoneName != "example.com" {
		t.Fatalf("phase=%q resources=%+v desired=%+v", s.Phase, s.Resources, s.Desired)
	}
	// A zone is never Aura's to delete: only the reference goes.
	if h.cloud.deletes != 0 || h.cloud.zone.ID == "" || h.cloud.creates != creates {
		t.Fatalf("deletes=%d zone=%+v creates %d to %d", h.cloud.deletes, h.cloud.zone, creates, h.cloud.creates)
	}
}

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
		} else if err != nil || h.cloud.deletes != 3 || h.store.state.Resources.TunnelID != "" {
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
	for _, resource := range []string{"tunnel", "public-app", "public-policy", "public-dns", "marker"} {
		t.Run(resource, func(t *testing.T) {
			h := newHarness(t)
			h.assertResources(t)
			switch resource {
			case "tunnel":
				h.cloud.tunnel.Name = "foreign"
			case "public-app":
				a := h.cloud.apps[resource]
				a.Name = "foreign"
				h.cloud.apps[resource] = a
			case "public-policy":
				p := h.cloud.policies[resource]
				p.Name = "foreign"
				h.cloud.policies[resource] = p
			case "public-dns":
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
	delete(h.cloud.dns, "public-dns")
	delete(h.cloud.policies, "public-policy")
	delete(h.cloud.apps, "public-app")
	if err := h.r.Delete(t.Context(), "admin"); err != nil {
		t.Fatal(err)
	}
	if h.cloud.deletes != 1 || h.store.state.Phase != PhaseDisabled {
		t.Fatal("absence reconciliation failed")
	}
}

func TestDeleteResumesAndPreservesZoneAndOTP(t *testing.T) {
	h := newHarness(t)
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	h.cloud.failDeleteAfter = 2
	if err := h.r.Delete(t.Context(), "admin"); err == nil {
		t.Fatal("expected interruption")
	}
	h.cloud.failDeleteAfter = 0
	h.r = h.newReconciler()
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if h.cloud.deletes != 4 || h.cloud.zone.ID == "" || h.cloud.otp.ID == "" || h.store.state.Resources != (Resources{AccountID: "account"}) || h.store.state.Phase != PhaseDisabled {
		t.Fatalf("delete incomplete: %+v count=%d", h.store.state, h.cloud.deletes)
	}
}
