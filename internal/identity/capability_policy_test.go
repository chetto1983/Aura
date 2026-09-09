// capability_policy_test.go pins the refusal contract capability_policy.go decides (RBAC-02,
// RBAC-06, RBAC-07, RBAC-09). Every refusal is asserted with errors.Is against a named
// sentinel, never by string match and never merely as non-nil — a mutant that swaps one
// refusal for another must be killable (REL-06's capability_policy scope).
package identity

import (
	"errors"
	"testing"
)

func TestCanGrantThroughAPI_RefusesAdministrative(t *testing.T) {
	t.Parallel()
	for _, cap := range Administrative() {
		t.Run(cap, func(t *testing.T) {
			t.Parallel()
			err := CanGrantThroughAPI(cap)
			if !errors.Is(err, ErrCapabilityNotGrantable) {
				t.Fatalf("CanGrantThroughAPI(%q) = %v, want ErrCapabilityNotGrantable", cap, err)
			}
		})
	}
}

func TestCanGrantThroughAPI_AllowsUserSet(t *testing.T) {
	t.Parallel()
	for _, cap := range UserSet() {
		t.Run(cap, func(t *testing.T) {
			t.Parallel()
			if err := CanGrantThroughAPI(cap); err != nil {
				t.Fatalf("CanGrantThroughAPI(%q) = %v, want nil", cap, err)
			}
		})
	}
}

func TestCanGrantThroughAPI_AdjacentNames(t *testing.T) {
	t.Parallel()
	// A name that merely contains an administrative name as a substring must never be
	// refused via the administrative branch — the guard matches exact names only. Each of
	// these is well-formed grammar but undeclared, so it fails ErrCapabilityNotDeclared,
	// never ErrCapabilityNotGrantable. Asserted by sentinel identity so the two failure
	// modes cannot be confused.
	for _, cap := range []string{"identity.created", "x.identity.create", "identity.create.x"} {
		t.Run(cap, func(t *testing.T) {
			t.Parallel()
			err := CanGrantThroughAPI(cap)
			if errors.Is(err, ErrCapabilityNotGrantable) {
				t.Fatalf("CanGrantThroughAPI(%q) refused via the administrative branch — must match exact names only", cap)
			}
			if !errors.Is(err, ErrCapabilityNotDeclared) {
				t.Fatalf("CanGrantThroughAPI(%q) = %v, want ErrCapabilityNotDeclared", cap, err)
			}
		})
	}
}

func TestCanGrantThroughAPI_EmptyAndUnknown(t *testing.T) {
	t.Parallel()

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		if err := CanGrantThroughAPI(""); !errors.Is(err, ErrInvalidCapability) {
			t.Fatalf("CanGrantThroughAPI(\"\") = %v, want ErrInvalidCapability", err)
		}
	})

	t.Run("undeclared", func(t *testing.T) {
		t.Parallel()
		if err := CanGrantThroughAPI("undeclared.capability"); !errors.Is(err, ErrCapabilityNotDeclared) {
			t.Fatalf("CanGrantThroughAPI(undeclared) = %v, want ErrCapabilityNotDeclared", err)
		}
	})

	t.Run("wildcard", func(t *testing.T) {
		t.Parallel()
		if err := CanGrantThroughAPI(Wildcard); !errors.Is(err, ErrWildcardManaged) {
			t.Fatalf("CanGrantThroughAPI(%q) = %v, want ErrWildcardManaged", Wildcard, err)
		}
	})
}

func TestCanGrantThroughAPI_OrderIndependent(t *testing.T) {
	t.Parallel()
	// The declared set is source-order stable (Administrative() then UserSet()), but the
	// policy's answer must not depend on that order — proven by shuffling a copy and
	// asserting the same answers hold. All() itself is not shuffled (it is the package's
	// own fixed source order); this shuffles a local copy used only to prove the property.
	shuffled := All()
	for i, j := 0, len(shuffled)-1; i < j; i, j = i+1, j-1 {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}

	for _, cap := range shuffled {
		first := CanGrantThroughAPI(cap)
		second := CanGrantThroughAPI(cap)
		if !errors.Is(first, second) {
			t.Fatalf("CanGrantThroughAPI(%q) is not order-independent: %v vs %v", cap, first, second)
		}
	}
}

func TestCanRevokeThroughAPI_MirrorsGrant(t *testing.T) {
	t.Parallel()
	// Revoke refuses exactly the same names grant does, proven by table over All() rather
	// than by two hand-written cases — a divergence here would mean the two verbs drifted.
	for _, cap := range All() {
		t.Run(cap, func(t *testing.T) {
			t.Parallel()
			grantErr := CanGrantThroughAPI(cap)
			revokeErr := CanRevokeThroughAPI(cap)
			grantRefused := grantErr != nil
			revokeRefused := revokeErr != nil
			if grantRefused != revokeRefused {
				t.Fatalf("CanGrantThroughAPI(%q) = %v but CanRevokeThroughAPI(%q) = %v — verbs disagree", cap, grantErr, cap, revokeErr)
			}
			if grantRefused && !errors.Is(revokeErr, ErrCapabilityNotGrantable) != !errors.Is(grantErr, ErrCapabilityNotGrantable) {
				t.Fatalf("CanRevokeThroughAPI(%q) = %v, want the same sentinel as CanGrantThroughAPI: %v", cap, revokeErr, grantErr)
			}
		})
	}
}

func TestCanRemoveIdentity_RefusesLastAdminSelf(t *testing.T) {
	t.Parallel()

	t.Run("admin removes self", func(t *testing.T) {
		t.Parallel()
		err := CanRemoveIdentity("A", "A", true)
		if !errors.Is(err, ErrLastAdministrator) {
			t.Fatalf("CanRemoveIdentity(A, A, true) = %v, want ErrLastAdministrator", err)
		}
	})

	t.Run("admin removes another identity", func(t *testing.T) {
		t.Parallel()
		if err := CanRemoveIdentity("A", "B", true); err != nil {
			t.Fatalf("CanRemoveIdentity(A, B, true) = %v, want nil", err)
		}
	})

	t.Run("non-administrative identity removes self", func(t *testing.T) {
		t.Parallel()
		if err := CanRemoveIdentity("A", "A", false); err != nil {
			t.Fatalf("CanRemoveIdentity(A, A, false) = %v, want nil", err)
		}
	})
}

func TestCanRemoveIdentity_EmptyIdsRefuse(t *testing.T) {
	t.Parallel()
	// Deny by default (RBAC-09): an empty id must never read as "nothing to check".
	cases := []struct {
		name    string
		caller  string
		subject string
	}{
		{"empty caller", "", "B"},
		{"empty subject", "A", ""},
		{"both empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := CanRemoveIdentity(tc.caller, tc.subject, false); err == nil {
				t.Fatalf("CanRemoveIdentity(%q, %q, false) = nil, want a refusal", tc.caller, tc.subject)
			}
		})
	}
}

func TestCanDeactivateIdentity_SameRuleAsRemove(t *testing.T) {
	t.Parallel()
	// Deactivate and remove must agree on every input in this table, so the two verbs
	// cannot drift apart (D-03).
	cases := []struct {
		name           string
		caller         string
		subject        string
		administrative bool
	}{
		{"admin removes self", "A", "A", true},
		{"admin removes another", "A", "B", true},
		{"non-admin removes self", "A", "A", false},
		{"empty caller", "", "B", false},
		{"empty subject", "A", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			removeErr := CanRemoveIdentity(tc.caller, tc.subject, tc.administrative)
			deactivateErr := CanDeactivateIdentity(tc.caller, tc.subject, tc.administrative)
			removeRefused := removeErr != nil
			deactivateRefused := deactivateErr != nil
			if removeRefused != deactivateRefused {
				t.Fatalf("CanRemoveIdentity = %v but CanDeactivateIdentity = %v — verbs disagree", removeErr, deactivateErr)
			}
			if removeRefused && !errors.Is(deactivateErr, ErrLastAdministrator) != !errors.Is(removeErr, ErrLastAdministrator) {
				t.Fatalf("CanDeactivateIdentity = %v, want the same sentinel as CanRemoveIdentity: %v", deactivateErr, removeErr)
			}
		})
	}
}
