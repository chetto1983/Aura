package agui

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/identity"
)

// onboarding_provision_grants.go is the provisioning saga's grant concern, split out of
// onboarding_provision.go (C-05: that file was 568 lines and Task 2 grows it, so it must be
// split before it grows further — refactor before, not after). It holds the creator
// pre-validation that used to live at onboarding_provision.go:481-510 and the uniform-grant
// semantics D-01/RBAC-03 introduce: every identity provisioned through the onboarding saga
// receives exactly identity.UserSet() — agent.run, governance.read, governance.write,
// share.public — and neither administrative capability, whatever the provision request
// asked for. The request no longer SELECTS the grants; the policy does.

// identityCreateCapability is the capability_grants name the create mutation is gated on
// (ONBD-01a / D-04, parity with agent.run). The route mount enforces it via
// RequireCapability; the saga re-checks it (belt-and-suspenders) so the creator must hold
// identity.create explicitly to provision (RBAC-01: the wildcard is retired as of migration
// 0121, so a bare '*' row no longer satisfies this check). Alias of
// internal/identity.CapIdentityCreate (RBAC-02) — never a re-declared literal.
const identityCreateCapability = identity.CapIdentityCreate

// validateProvisionCapabilities is the server-side pre-check before any write (D-01/RBAC-03,
// belt-and-suspenders behind RequireCapability at the route mount): the creator must hold
// identity.create, and a request naming either administrative capability
// (identity.create/identity.delete) is refused rather than silently narrowed — an admin who
// tries to create a second admin through provisioning learns that it is impossible instead
// of believing it worked.
//
// Every OTHER requested name — well-formed, malformed, undeclared, even the retired '*'
// wildcard — is deliberately NOT validated here: since the grant this saga performs is
// always identity.UserSet() (Provision, Leg A), the request's own list no longer selects
// anything, so anything short of an administrative name is harmless noise, not a validation
// target. Renamed from the pre-Phase-2 validateNoEscalation, whose subset-of-creator-grants
// check is retired with it — D-01 made every non-administrative capability universal, so
// there is no longer a narrower creator grant set to be a subset of.
func (s *onboardingService) validateProvisionCapabilities(ctx context.Context, creator string, requested []string) error {
	if creator == "" {
		return errOnboardingForbidden
	}
	grants, err := s.caps.ListCapabilities(ctx, creator)
	if err != nil {
		return provisionFail("list creator capabilities", err)
	}
	creatorSet := make(map[string]bool, len(grants))
	for _, g := range grants {
		creatorSet[g] = true
	}
	// The creator must be authorized to create identities (route gate backstop).
	if !creatorSet[identityCreateCapability] {
		return errOnboardingForbidden
	}
	for _, c := range requested {
		if identity.IsAdministrative(c) {
			return fmt.Errorf("%w: %q is administrative and cannot be requested through provisioning", ErrOnboardingEscalation, c)
		}
	}
	return nil
}
