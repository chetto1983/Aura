package agui

import (
	"context"
	"testing"
)

type fakeKeyDisabler struct{ disabled []string }

func (f *fakeKeyDisabler) DisableKey(_ context.Context, identityID string) error {
	f.disabled = append(f.disabled, identityID)
	return nil
}

// TestDeprovisionDeactivateDisablesTheOpenRouterKey proves a deactivated identity cannot spend
// through the grace window before the purge revokes its key.
func TestDeprovisionDeactivateDisablesTheOpenRouterKey(t *testing.T) {
	deps, f := fullDeprovisionDeps()
	f.deact.targets[testIdentityID] = targetFor(testIdentityID)
	disabler := &fakeKeyDisabler{}
	deps.KeyDisabler = disabler

	if err := NewDeprovisioner(deps).Deactivate(context.Background(), testIdentityID); err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	if len(disabler.disabled) != 1 || disabler.disabled[0] != testIdentityID {
		t.Fatalf("disabled = %v, want [%s]", disabler.disabled, testIdentityID)
	}
}
