// identity_deprovision.go implements `aura identity {deactivate|purge}` — the missing
// entry point onto the D-27 de-provisioning saga (internal/agui/deprovision.go), which has
// shipped, journaled, idempotent and resumable since Phase 36 with only the cron
// grace-window sweep able to reach it. No second teardown path, no SQL DELETE: a raw
// delete would cascade the Postgres catalog and leave the identity's ArcadeDB database,
// its Garage bucket+key, its filesystem roots and its Authula user orphaned with no owner
// row left to find them by.
//
// RED SKELETON — the seams are declared so the specification in
// identity_deprovision_test.go compiles and fails on its assertions rather than on the
// compiler. The implementation lands in the next commit.
package main

import (
	"context"
	"errors"

	"github.com/chetto1983/aura/internal/identity"
)

type deprovisionVerb string

const (
	deprovisionVerbDeactivate deprovisionVerb = "deactivate"
	deprovisionVerbPurge      deprovisionVerb = "purge"
)

// errProtectedIdentity is the refusal for a target that must never be torn down.
var errProtectedIdentity = errors.New("identity is protected and cannot be deprovisioned")

// identityLookup is the narrow read seam the resolver needs, satisfied by *identity.Store.
type identityLookup interface {
	GetIdentityByName(ctx context.Context, name string) (identity.Identity, error)
	GetIdentityByID(ctx context.Context, identityID string) (identity.Identity, error)
	ListCapabilities(ctx context.Context, identityID string) ([]string, error)
}

// identityDeprovisioner is the narrow seam onto the shipped saga, satisfied by
// *agui.Deprovisioner.
type identityDeprovisioner interface {
	Deactivate(ctx context.Context, identityID string) error
	PurgeOne(ctx context.Context, identityID string) error
}

func parseIdentityDeprovisionArgs(_ []string) (string, error) { return "", nil }

func resolveDeprovisionTarget(_ context.Context, _ identityLookup, _ string) (identity.Identity, error) {
	return identity.Identity{}, nil
}

func guardProtectedIdentity(_ identity.Identity, _ []string) error { return nil }

func runIdentityDeprovision(_ context.Context, _ identityDeprovisioner, _ deprovisionVerb, _ string) error {
	return nil
}
