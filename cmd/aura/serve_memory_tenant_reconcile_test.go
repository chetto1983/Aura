package main

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/identity"
)

type fakeMemoryIdentityLister struct {
	identities []identity.Identity
}

func (f fakeMemoryIdentityLister) ListIdentities(context.Context) ([]identity.Identity, error) {
	return f.identities, nil
}

type recordingMemoryProvisioner struct {
	provisioned []string
}

func (f *recordingMemoryProvisioner) ProvisionMemory(_ context.Context, identityID string) error {
	f.provisioned = append(f.provisioned, identityID)
	return nil
}

func (*recordingMemoryProvisioner) PurgeMemory(context.Context, string) error { return nil }

func TestReconcileArcadeMemoryTenantsProvisionsOnlyActiveUsers(t *testing.T) {
	lister := fakeMemoryIdentityLister{identities: []identity.Identity{
		{ID: "00000000-0000-0000-0000-000000000001", Kind: "system"},
		{ID: "11111111-1111-4111-8111-111111111111", Kind: "user"},
		{ID: "22222222-2222-4222-8222-222222222222", Kind: "service"},
		{ID: "33333333-3333-4333-8333-333333333333", Kind: "user", Deactivated: true},
		{ID: "44444444-4444-4444-8444-444444444444", Kind: "user"},
	}}
	memory := &recordingMemoryProvisioner{}

	if err := reconcileArcadeMemoryTenants(context.Background(), lister, memory); err != nil {
		t.Fatalf("reconcileArcadeMemoryTenants: %v", err)
	}
	want := []string{"11111111-1111-4111-8111-111111111111", "44444444-4444-4444-8444-444444444444"}
	if len(memory.provisioned) != len(want) {
		t.Fatalf("provisioned=%v, want %v", memory.provisioned, want)
	}
	for i := range want {
		if memory.provisioned[i] != want[i] {
			t.Fatalf("provisioned=%v, want %v", memory.provisioned, want)
		}
	}
}
