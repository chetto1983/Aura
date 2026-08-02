package main

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// testIdentity is a real-shaped Aura identity; databaseFor refuses anything that
// is not one, so a test cannot accidentally rely on a lax name.
const testIdentity = "00000000-0000-0000-0000-000000000001"

// singleTenant pre-seeds the resolver's cache so a unit test drives one recording
// client without provisioning a database. It goes through the same map the
// production path fills, so the lookup under test is the real one.
func singleTenant(t *testing.T, client *arcadedb.Client) *tenants {
	t.Helper()
	database, err := databaseFor(testIdentity)
	if err != nil {
		t.Fatalf("databaseFor: %v", err)
	}
	tn := newTenants(arcadedb.Config{}, nil, nil, nil)
	tn.clients[database] = client
	return tn
}

func TestDatabaseForIsolatesAndFailsClosed(t *testing.T) {
	t.Parallel()
	first, err := databaseFor("11111111-1111-1111-1111-111111111111")
	if err != nil {
		t.Fatalf("databaseFor: %v", err)
	}
	second, err := databaseFor("22222222-2222-2222-2222-222222222222")
	if err != nil {
		t.Fatalf("databaseFor: %v", err)
	}
	if first == second {
		t.Fatal("two identities mapped to one database")
	}
	// Dashes are not legal in an ArcadeDB database name; the mapping must be
	// total, not merely usually-fine.
	for _, name := range []string{first, second} {
		for _, r := range name {
			if r != '_' && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
				t.Fatalf("database name %q carries %q", name, r)
			}
		}
	}
	// An unstamped call must be REFUSED. The alternative — treating empty as "the
	// shared database" — is exactly the hole a per-tenant design exists to close:
	// one forgotten stamp and a caller reads everyone's memory.
	if _, err := databaseFor("   "); err == nil {
		t.Error("an empty identity was accepted")
	}
	for _, bad := range []string{"../etc", "name with spaces", "'; DROP DATABASE x --", "a"} {
		if _, err := databaseFor(bad); err == nil {
			t.Errorf("identity %q was accepted as a database name", bad)
		}
	}
}

func TestTenantsRefusesAnUnstampedCall(t *testing.T) {
	t.Parallel()
	if _, err := newTenants(arcadedb.Config{}, nil, nil, nil).For(context.Background(), ""); err == nil {
		t.Fatal("For(\"\") returned a client; memory must be per identity")
	}
}

func TestTenantCredentialsAreDerivedAndDistinct(t *testing.T) {
	t.Setenv(tenantSecretEnv, "a-secret-long-enough-to-be-a-real-key-0123456789")
	creds, err := newTenantCredentials()
	if err != nil {
		t.Fatalf("newTenantCredentials: %v", err)
	}
	first, _ := databaseFor("11111111-1111-1111-1111-111111111111")
	second, _ := databaseFor("22222222-2222-2222-2222-222222222222")

	// Derived, so the same identity always reproduces its own credential without
	// anything being stored. Taken through a second constructor over the same
	// secret rather than calling the same method twice — that reads as a tautology
	// and is what a caller actually depends on: a restarted sidecar must derive the
	// credential it never wrote down.
	again, err := newTenantCredentials()
	if err != nil {
		t.Fatalf("newTenantCredentials: %v", err)
	}
	if again.passwordFor(first) != creds.passwordFor(first) {
		t.Fatal("derivation is not reproducible across constructions")
	}
	if creds.passwordFor(first) == creds.passwordFor(second) {
		t.Fatal("two identities derived the same password")
	}
	if creds.userFor(first) == creds.userFor(second) {
		t.Fatal("two identities derived the same user")
	}
	if len(creds.passwordFor(first)) < 40 {
		t.Fatalf("derived password is %d chars", len(creds.passwordFor(first)))
	}
	// A different secret must produce different credentials, or rotation does
	// nothing.
	t.Setenv(tenantSecretEnv, "a-DIFFERENT-secret-long-enough-to-be-real-01234567")
	rotated, err := newTenantCredentials()
	if err != nil {
		t.Fatalf("newTenantCredentials: %v", err)
	}
	if rotated.passwordFor(first) == creds.passwordFor(first) {
		t.Fatal("rotating the secret did not change the derived credential")
	}
}

func TestTenantCredentialsFailClosed(t *testing.T) {
	// No secret and a short secret must both REFUSE. Falling back to a shared
	// credential would leave every identity reading one memory, silently — the
	// exact failure the per-tenant design exists to prevent.
	t.Setenv(tenantSecretEnv, "")
	if _, err := newTenantCredentials(); err == nil {
		t.Error("an absent secret was accepted")
	}
	t.Setenv(tenantSecretEnv, "too-short")
	if _, err := newTenantCredentials(); err == nil {
		t.Error("a short secret was accepted")
	}
}
