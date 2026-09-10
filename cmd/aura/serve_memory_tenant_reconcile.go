package main

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/identity"
)

type memoryIdentityLister interface {
	ListIdentities(context.Context) ([]identity.Identity, error)
}

// isActiveHuman is the one test for "a person this box serves": a `service` identity such
// as aura-cli and the seeded `system` row have no memory tenant, no cockpit and no sidecar
// data of their own, and a deactivated user is on its way out.
func isActiveHuman(row identity.Identity) bool {
	return row.Kind == identityKindUser && !row.Deactivated
}

// reconcileArcadeMemoryTenants repairs identities created before eager ArcadeDB
// provisioning existed. TenantClients makes every step idempotent, so the same pass
// is also a safe startup integrity check for already-provisioned users.
func reconcileArcadeMemoryTenants(
	ctx context.Context,
	identities memoryIdentityLister,
	memory agui.MemoryProvisioner,
) error {
	if memory == nil {
		return nil
	}
	if identities == nil {
		return fmt.Errorf("reconcile ArcadeDB tenants: identity source is not configured")
	}
	rows, err := identities.ListIdentities(ctx)
	if err != nil {
		return fmt.Errorf("reconcile ArcadeDB tenants: list identities: %w", err)
	}
	for _, row := range rows {
		if !isActiveHuman(row) {
			continue
		}
		if err := memory.ProvisionMemory(ctx, row.ID); err != nil {
			return fmt.Errorf("reconcile ArcadeDB tenant %s: %w", row.ID, err)
		}
	}
	return nil
}
