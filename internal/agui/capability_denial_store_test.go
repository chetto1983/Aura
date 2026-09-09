package agui

import (
	"testing"

	"github.com/google/uuid"
)

// TestDenialCauseConstantsAreDistinct pins the closed three-value cause vocabulary
// RequireCapability's three refusal branches map onto (internal/agui/auth.go) — the same
// set the migration's CHECK constraint enforces at the database layer.
func TestDenialCauseConstantsAreDistinct(t *testing.T) {
	causes := []string{DenialCauseNoPrincipal, DenialCauseStoreError, DenialCauseNotHeld}
	seen := make(map[string]bool, len(causes))
	for _, c := range causes {
		if c == "" {
			t.Fatalf("cause constant is empty: %v", causes)
		}
		if seen[c] {
			t.Fatalf("cause constant %q is not distinct: %v", c, causes)
		}
		seen[c] = true
	}
}

// TestNoPrincipalIdentityIDIsNotAValidUUID pins that the documented no-principal sentinel
// can never collide with a real identity id: it is text, not a uuid, on purpose.
func TestNoPrincipalIdentityIDIsNotAValidUUID(t *testing.T) {
	if NoPrincipalIdentityID == "" {
		t.Fatal("NoPrincipalIdentityID must not be empty")
	}
	if _, err := uuid.Parse(NoPrincipalIdentityID); err == nil {
		t.Fatalf("NoPrincipalIdentityID %q parses as a UUID — it must never collide with a real identity id", NoPrincipalIdentityID)
	}
}
