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
		if row.Kind != "user" || row.Deactivated {
			continue
		}
		if err := memory.ProvisionMemory(ctx, row.ID); err != nil {
			return fmt.Errorf("reconcile ArcadeDB tenant %s: %w", row.ID, err)
		}
	}
	return nil
}
