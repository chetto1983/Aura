package agui

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
)

// deprovision_last_admin_test.go proves T-02-04's mitigation (D-03, RBAC-07): the last
// administrative identity cannot remove or deactivate itself, through either Deactivate or
// Purge, enforced once inside the saga so the CLI and the plan-02-07 HTTP route share the
// same refusal. targetFor/fullDeprovisionDeps/testIdentityID are shared with
// deprovision_test.go (same package).

const lastAdminTestID = "33333333-3333-4333-8333-333333333333"

// adminTargetFor mirrors targetFor but stamps IsAdministrative — the field
// CanDeactivateIdentity/CanRemoveIdentity read to decide the last-administrator refusal.
func adminTargetFor(id string) DeprovisionTarget {
	t := targetFor(id)
	t.IsAdministrative = true
	return t
}

// asAdminSelf builds a context whose principal equals subjectID — the caller removing or
// deactivating itself.
func asAdminSelf(subjectID string) context.Context {
	return identityctx.WithIdentityID(context.Background(), subjectID)
}

func TestLastAdminCannotRemoveSelf(t *testing.T) {
	t.Run("Deactivate refuses admin self-removal, no write", func(t *testing.T) {
		deps, f := fullDeprovisionDeps()
		f.deact.targets[lastAdminTestID] = adminTargetFor(lastAdminTestID)
		d := NewDeprovisioner(deps)

		err := d.Deactivate(asAdminSelf(lastAdminTestID), lastAdminTestID)
		if !errors.Is(err, identity.ErrLastAdministrator) {
			t.Fatalf("Deactivate(self, admin) = %v, want ErrLastAdministrator", err)
		}
		if _, marked := f.deact.marked[lastAdminTestID]; marked {
			t.Fatal("MarkDeactivated was called despite the refusal")
		}
		if f.sess.count() != 0 || f.jobs.count() != 0 {
			t.Fatalf("KillSessions/TerminateJobs called despite the refusal: sessions=%d jobs=%d", f.sess.count(), f.jobs.count())
		}
	})

	t.Run("Purge refuses admin self-removal, no write", func(t *testing.T) {
		deps, f := fullDeprovisionDeps()
		target := adminTargetFor(lastAdminTestID)
		d := NewDeprovisioner(deps)

		err := d.Purge(asAdminSelf(lastAdminTestID), target)
		if !errors.Is(err, identity.ErrLastAdministrator) {
			t.Fatalf("Purge(self, admin) = %v, want ErrLastAdministrator", err)
		}
		if f.conv.count() != 0 || f.memory.count() != 0 || f.iddel.count() != 0 || f.authdel.count() != 0 {
			t.Fatalf("Purge wrote despite the refusal: conv=%d memory=%d idDelete=%d authula=%d",
				f.conv.count(), f.memory.count(), f.iddel.count(), f.authdel.count())
		}
	})

	t.Run("admin removing a DIFFERENT identity is not refused by this rule", func(t *testing.T) {
		deps, f := fullDeprovisionDeps()
		f.deact.targets[testIdentityID] = targetFor(testIdentityID) // subject is non-administrative
		d := NewDeprovisioner(deps)

		if err := d.Deactivate(asAdminSelf(lastAdminTestID), testIdentityID); err != nil {
			t.Fatalf("Deactivate(admin acting on another identity) = %v, want nil", err)
		}
		if _, marked := f.deact.marked[testIdentityID]; !marked {
			t.Fatal("MarkDeactivated was not called for a legitimately-permitted deactivation")
		}
	})

	t.Run("non-administrative identity may deactivate itself", func(t *testing.T) {
		deps, f := fullDeprovisionDeps()
		f.deact.targets[testIdentityID] = targetFor(testIdentityID) // IsAdministrative: false
		d := NewDeprovisioner(deps)

		if err := d.Deactivate(asAdminSelf(testIdentityID), testIdentityID); err != nil {
			t.Fatalf("Deactivate(non-admin self) = %v, want nil", err)
		}
		if _, marked := f.deact.marked[testIdentityID]; !marked {
			t.Fatal("MarkDeactivated was not called for a non-administrative self-deactivation")
		}
	})

	// The grace-window cron sweep and the CLI run headless (no context principal). The
	// caller resolves to headlessDeprovisionCaller, which can never equal a real identity
	// id, so the refusal never fires and the sweep continues to converge — a break here
	// would silently strand every deactivated identity past its grace window.
	t.Run("headless cron sweep purging an admin target is unaffected by this rule", func(t *testing.T) {
		deps, f := fullDeprovisionDeps()
		target := adminTargetFor(lastAdminTestID)
		d := NewDeprovisioner(deps)

		if err := d.Purge(context.Background(), target); err != nil {
			t.Fatalf("headless Purge(admin target) = %v, want nil (the sweep must not be refused)", err)
		}
		if f.iddel.count() != 1 {
			t.Fatalf("headless sweep did not purge the admin target: idDelete=%d, want 1", f.iddel.count())
		}
	})
}

// TestLastAdminSelfRemovalIsIdempotentlyRefused proves a retry never succeeds on the
// second attempt (RBAC-07 idempotency truth): two sequential self-removal attempts by the
// same administrative identity both refuse, and neither writes.
func TestLastAdminSelfRemovalIsIdempotentlyRefused(t *testing.T) {
	deps, f := fullDeprovisionDeps()
	f.deact.targets[lastAdminTestID] = adminTargetFor(lastAdminTestID)
	d := NewDeprovisioner(deps)

	ctx := asAdminSelf(lastAdminTestID)
	first := d.Deactivate(ctx, lastAdminTestID)
	second := d.Deactivate(ctx, lastAdminTestID)

	if !errors.Is(first, identity.ErrLastAdministrator) || !errors.Is(second, identity.ErrLastAdministrator) {
		t.Fatalf("two sequential self-removal attempts = (%v, %v), want ErrLastAdministrator both times", first, second)
	}
	if _, marked := f.deact.marked[lastAdminTestID]; marked {
		t.Fatal("MarkDeactivated was called on a retried refusal — deactivated_at must stay unset")
	}
}
