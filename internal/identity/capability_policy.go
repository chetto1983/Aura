// capability_policy.go is the single pure file that decides every refusal this phase adds
// (RBAC-02, RBAC-06, RBAC-07, RBAC-09). It has zero I/O: no context, no database, no
// logging, no HTTP status. It decides; the caller maps the sentinel to a transport-level
// response. Keeping the mapping out of this file means a mutation of the decision itself
// cannot be masked by the mapping (REL-06's capability_policy mutation scope,
// scripts/critical_mutation_gate.py — one file, one score).
package identity

import (
	"errors"
	"fmt"
	"slices"
)

// Sentinels declared beside ErrWildcardManaged/ErrInvalidCapability (store.go) — the same
// naming convention, so a caller classifies every refusal this package can return without
// string matching.
var (
	// ErrCapabilityNotGrantable is returned when the requested capability is one of the two
	// administrative names (identity.create, identity.delete). Administrative capabilities
	// are never grantable through the API — admin is bootstrap-only.
	ErrCapabilityNotGrantable = errors.New("administrative capabilities are not grantable through the API; admin is bootstrap-only")

	// ErrCapabilityNotDeclared is returned when a well-formed capability name is not in the
	// declared set (All()) — distinct from ErrInvalidCapability (bad grammar) so the two
	// failure modes are never confused.
	ErrCapabilityNotDeclared = errors.New("capability is not declared")

	// ErrLastAdministrator is returned when the caller is removing or deactivating itself
	// and the caller is administrative. Unconditional, not a count: admin is bootstrap-only
	// and there is exactly one, so counting remaining admins would silently permit
	// self-removal the moment a second admin appeared — and a second admin cannot appear
	// (D-03).
	ErrLastAdministrator = errors.New("the last administrative identity cannot remove or deactivate itself; use `aura identity` on the host")

	// ErrEmptyIdentityID is returned when either identity id passed to CanRemoveIdentity or
	// CanDeactivateIdentity is empty — deny by default (RBAC-09), never a silent pass.
	ErrEmptyIdentityID = errors.New("identity id must not be empty")
)

// capabilityAPIPolicy is the single predicate CanGrantThroughAPI and CanRevokeThroughAPI both
// delegate to, so the two verbs cannot drift apart (D-02) and dupl (threshold 100) never has
// two copies of this logic to flag. Refuses in order: bad grammar/wildcard, undeclared,
// administrative. Each refusal is a distinct sentinel so a mutant swapping one for another is
// killable.
func capabilityAPIPolicy(capability string) error {
	if err := ValidateCapabilityName(capability); err != nil {
		return err
	}
	if !slices.Contains(All(), capability) {
		return fmt.Errorf("%w: %q", ErrCapabilityNotDeclared, capability)
	}
	if IsAdministrative(capability) {
		return ErrCapabilityNotGrantable
	}
	return nil
}

// CanGrantThroughAPI reports whether capability may be granted through
// POST /api/admin/identities/{id}/capabilities. Refuses the wildcard, a name failing
// ValidateCapabilityName, a name not in All(), and either administrative name — for every
// caller, including one who already holds it (D-02, RBAC-06).
func CanGrantThroughAPI(capability string) error {
	return capabilityAPIPolicy(capability)
}

// CanRevokeThroughAPI reports whether capability may be revoked through
// DELETE /api/admin/identities/{id}/capabilities/{capability}. Same contract as
// CanGrantThroughAPI — delegates to the shared predicate rather than copying it, so the two
// verbs cannot drift apart.
func CanRevokeThroughAPI(capability string) error {
	return capabilityAPIPolicy(capability)
}

// canRemoveOrDeactivate is the single predicate CanRemoveIdentity and CanDeactivateIdentity
// both delegate to, so the two verbs agree on every input (D-03).
func canRemoveOrDeactivate(callerIdentityID, subjectIdentityID string, subjectIsAdministrative bool) error {
	if callerIdentityID == "" || subjectIdentityID == "" {
		return ErrEmptyIdentityID
	}
	if callerIdentityID == subjectIdentityID && subjectIsAdministrative {
		return ErrLastAdministrator
	}
	return nil
}

// CanRemoveIdentity reports whether callerIdentityID may remove subjectIdentityID. Refuses
// when either id is empty (deny by default, RBAC-09) and when the caller is the subject AND
// the subject is administrative (D-03: unconditional — do NOT count remaining admins).
func CanRemoveIdentity(callerIdentityID, subjectIdentityID string, subjectIsAdministrative bool) error {
	return canRemoveOrDeactivate(callerIdentityID, subjectIdentityID, subjectIsAdministrative)
}

// CanDeactivateIdentity has the identical contract to CanRemoveIdentity, delegating to the
// same predicate so deactivate and remove cannot drift apart.
func CanDeactivateIdentity(callerIdentityID, subjectIdentityID string, subjectIsAdministrative bool) error {
	return canRemoveOrDeactivate(callerIdentityID, subjectIdentityID, subjectIsAdministrative)
}
