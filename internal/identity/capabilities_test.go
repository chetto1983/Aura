// Unit tier (no build tag): pure declaration logic, no database.
package identity

import (
	"testing"
)

// TestDeclaredCapabilities pins the declared capability set (RBAC-01/RBAC-02):
// Administrative() is exactly the two administrative names, UserSet() is
// exactly the four user names, All() is their concatenation in source order
// with no duplicates, and every declared name passes the capability grammar.
func TestDeclaredCapabilities(t *testing.T) {
	t.Parallel()

	wantAdmin := []string{CapIdentityCreate, CapIdentityDelete}
	gotAdmin := Administrative()
	if !equalStrings(gotAdmin, wantAdmin) {
		t.Fatalf("Administrative() = %v, want %v", gotAdmin, wantAdmin)
	}

	wantUser := []string{CapAgentRun, CapGovernanceRead, CapGovernanceWrite, CapSharePublic}
	gotUser := UserSet()
	if !equalStrings(gotUser, wantUser) {
		t.Fatalf("UserSet() = %v, want %v", gotUser, wantUser)
	}

	wantAll := append(append([]string{}, wantAdmin...), wantUser...)
	gotAll := All()
	if !equalStrings(gotAll, wantAll) {
		t.Fatalf("All() = %v, want %v", gotAll, wantAll)
	}

	seen := make(map[string]bool, len(gotAll))
	for _, c := range gotAll {
		if seen[c] {
			t.Fatalf("All() contains duplicate %q", c)
		}
		seen[c] = true
		if err := ValidateCapabilityName(c); err != nil {
			t.Errorf("declared capability %q fails ValidateCapabilityName: %v", c, err)
		}
	}
}

// TestDeclaredCapabilities_ReturnsFreshCopy proves the slice-returning
// functions never hand back the package-level backing array — mutating a
// returned slice must not corrupt the next call's result.
func TestDeclaredCapabilities_ReturnsFreshCopy(t *testing.T) {
	t.Parallel()

	a := Administrative()
	if len(a) == 0 {
		t.Fatal("Administrative() returned empty slice, cannot probe mutation")
	}
	a[0] = "mutated"
	again := Administrative()
	if again[0] == "mutated" {
		t.Fatal("Administrative() leaked its backing array — a caller mutation corrupted the next call")
	}
}

// TestIsAdministrative_ExactMatch (RBAC-01 D-01): exact-string match only,
// never a prefix match, never case-folded.
func TestIsAdministrative_ExactMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cap  string
		want bool
	}{
		{"exact identity.create", "identity.create", true},
		{"exact identity.delete", "identity.delete", true},
		{"prefix-extended suffix rejected", "identity.creates", false},
		{"dotted suffix rejected", "identity.create.x", false},
		{"case-folded rejected", "IDENTITY.CREATE", false},
		{"empty rejected", "", false},
		{"user capability rejected", "agent.run", false},
		{"wildcard rejected", "*", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsAdministrative(tc.cap); got != tc.want {
				t.Errorf("IsAdministrative(%q) = %v, want %v", tc.cap, got, tc.want)
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
