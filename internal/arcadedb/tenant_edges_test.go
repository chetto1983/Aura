package arcadedb

import "testing"

// tenant_edges_test.go carries the edge battery tenant_test.go leaves open: adjacency
// (merge vs. separate), the specific charset-pattern lookalikes the header comment on
// tenant.go names ("--------", "0-0-0-0-0"), the TenantUserFor round trip, and
// DatabaseFor's own idempotency (tenant_test.go only asserts PasswordFor's). Every
// behaviour tenant_test.go already covers -- the basic empty/malformed refusal, two
// distinct UUIDs landing on different databases, PasswordFor's idempotency and
// distinctness, and the char-class of a produced name -- is left there rather than
// duplicated here (CLAUDE.md REUSABLE CODE); this file is untagged and needs no
// ArcadeDB, matching the delegated arcadedb_integration coverage authority's need for
// daemon-free unit tests over the package's pure derivation logic.

const (
	edgeIdentityA = "11111111-1111-1111-1111-111111111111"
	edgeIdentityB = "11111111-1111-1111-1111-111111111112" // one character different from A
	// edgeIdentityHex carries hex-letter digits (a-f) so an uppercase spelling actually
	// differs byte-for-byte from the canonical one -- edgeIdentityA is all-digit and would
	// make an "uppercase" test case a no-op.
	edgeIdentityHex = "aabbccdd-eeff-1234-5678-90abcdef1234"
)

// TestDatabaseForRefusesEmptyAndCharsetLookalikes: a blank identity and the specific
// charset-pattern strings tenant.go's own header comment says a naive charset check
// would have accepted ("--------", "0-0-0-0-0") must all be refused. Neither produces a
// usable database name.
func TestDatabaseForRefusesEmptyAndCharsetLookalikes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"whitespace only", "   "},
		{"dashes lookalike", "--------"},
		{"zeros-and-dashes lookalike", "0-0-0-0-0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db, err := DatabaseFor(tc.input)
			if err == nil {
				t.Fatalf("DatabaseFor(%q) = %q, want a refusal naming user_identifier", tc.input, db)
			}
			if db != "" {
				t.Fatalf("DatabaseFor(%q) returned a non-empty database %q alongside its error", tc.input, db)
			}
		})
	}
}

// TestDatabaseForAdjacencyMergesSpellingsSeparatesIdentities: two spellings of ONE UUID
// (uppercase, and surrounded by whitespace DatabaseFor trims before parsing) resolve to
// the SAME database name -- they merge. Two distinct UUIDs, including two that differ in
// a single character, resolve to DIFFERENT database names -- they separate. Neither
// collides with the other.
func TestDatabaseForAdjacencyMergesSpellingsSeparatesIdentities(t *testing.T) {
	t.Parallel()
	canonical, err := DatabaseFor(edgeIdentityHex)
	if err != nil {
		t.Fatalf("DatabaseFor(canonical): %v", err)
	}

	for _, tc := range []struct {
		name  string
		spell string
	}{
		{"uppercase spelling", "AABBCCDD-EEFF-1234-5678-90ABCDEF1234"},
		{"surrounded by whitespace", "  " + edgeIdentityHex + "  "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := DatabaseFor(tc.spell)
			if err != nil {
				t.Fatalf("DatabaseFor(%q): %v", tc.spell, err)
			}
			if got != canonical {
				t.Fatalf("DatabaseFor(%q) = %q, want the same database as the canonical spelling %q",
					tc.spell, got, canonical)
			}
		})
	}

	one, err := DatabaseFor(edgeIdentityA)
	if err != nil {
		t.Fatalf("DatabaseFor(A): %v", err)
	}
	other, err := DatabaseFor(edgeIdentityB)
	if err != nil {
		t.Fatalf("DatabaseFor(one-character-different): %v", err)
	}
	if other == one {
		t.Fatalf("DatabaseFor(%q) and DatabaseFor(%q) collided on %q despite differing by one character",
			edgeIdentityA, edgeIdentityB, one)
	}
	if one == canonical || other == canonical {
		t.Fatalf("DatabaseFor(A)=%q or DatabaseFor(B)=%q collided with the unrelated identity %q's database %q",
			one, other, edgeIdentityHex, canonical)
	}
}

// TestTenantUserForRoundTripsThroughDatabaseFor: TenantUserFor(DatabaseFor(id)) names a
// user derived from the SAME identity DatabaseFor was given -- stripping the "mem_"
// prefix DatabaseFor always writes and replacing it with "u_" must round-trip exactly,
// not merely produce SOME distinct-looking string.
func TestTenantUserForRoundTripsThroughDatabaseFor(t *testing.T) {
	t.Parallel()
	database, err := DatabaseFor(edgeIdentityA)
	if err != nil {
		t.Fatalf("DatabaseFor: %v", err)
	}
	want := "u_" + database[len("mem_"):]
	if got := TenantUserFor(database); got != want {
		t.Fatalf("TenantUserFor(%q) = %q, want %q", database, got, want)
	}
}

// TestDatabaseForIsIdempotent: called twice with the same input, DatabaseFor returns
// byte-identical results -- the derivation carries no state and no randomness. This is
// what lets a sidecar reproduce a database name it never wrote down; tenant_test.go
// asserts this property for PasswordFor but never for DatabaseFor itself.
func TestDatabaseForIsIdempotent(t *testing.T) {
	t.Parallel()
	first, err := DatabaseFor(edgeIdentityA)
	if err != nil {
		t.Fatalf("DatabaseFor (first call): %v", err)
	}
	second, err := DatabaseFor(edgeIdentityA)
	if err != nil {
		t.Fatalf("DatabaseFor (second call): %v", err)
	}
	if first != second {
		t.Fatalf("DatabaseFor(%q) returned %q then %q -- not idempotent", edgeIdentityA, first, second)
	}
}

// TestPasswordForIsIdempotentAcrossCalls: PasswordFor called twice on the same database
// name, from the SAME *TenantCredentials, returns byte-identical output. Distinct from
// tenant_test.go's TestTenantCredentialsAreDerivedAndDistinct, which asserts
// reproducibility ACROSS two separately-constructed TenantCredentials sharing a secret --
// this asserts it within one.
func TestPasswordForIsIdempotentAcrossCalls(t *testing.T) {
	t.Setenv(tenantSecretEnv, "edge-battery-secret-at-least-32-characters-long")
	creds, err := NewTenantCredentials()
	if err != nil {
		t.Fatalf("NewTenantCredentials: %v", err)
	}
	database, err := DatabaseFor(edgeIdentityA)
	if err != nil {
		t.Fatalf("DatabaseFor: %v", err)
	}
	first := creds.PasswordFor(database)
	second := creds.PasswordFor(database)
	if first != second {
		t.Fatalf("PasswordFor(%q) returned %q then %q -- not idempotent", database, first, second)
	}
}

// TestDatabaseForPrefixCannotBeForged: "mem_" is the ONLY prefix DatabaseFor ever writes,
// and it appears exactly once in any name it produces -- tenant.go's own header comment
// argues this holds because 'm' is not a hex digit, so no UUID's canonical hyphenated
// hex body can itself spell out "mem_". This asserts the argument's observable
// consequence directly, across several identities, rather than trusting the comment.
func TestDatabaseForPrefixCannotBeForged(t *testing.T) {
	t.Parallel()
	for _, id := range []string{
		edgeIdentityA,
		edgeIdentityB,
		"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		"deadbeef-dead-beef-dead-beefdeadbeef",
		"00000000-0000-0000-0000-000000000000",
		"ffffffff-ffff-ffff-ffff-ffffffffffff",
	} {
		database, err := DatabaseFor(id)
		if err != nil {
			t.Fatalf("DatabaseFor(%q): %v", id, err)
		}
		const prefix = "mem_"
		if database[:len(prefix)] != prefix {
			t.Fatalf("DatabaseFor(%q) = %q, does not start with %q", id, database, prefix)
		}
		body := database[len(prefix):]
		if containsMemPrefix(body) {
			t.Fatalf("DatabaseFor(%q) = %q, the identity body %q re-spells the %q prefix",
				id, database, body, prefix)
		}
	}
}

// containsMemPrefix reports whether s contains the literal substring "mem_" anywhere --
// the identity body is hex digits and underscores only, and 'm' is not a hex digit, so
// this must always be false; the test above fails loudly if that stops holding.
func containsMemPrefix(s string) bool {
	const needle = "mem_"
	if len(s) < len(needle) {
		return false
	}
	for i := 0; i+len(needle) <= len(s); i++ {
		if s[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
