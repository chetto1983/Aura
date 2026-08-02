package main

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// testIdentity is a real-shaped Aura identity; arcadedb.DatabaseFor refuses
// anything that is not one, so a test cannot accidentally rely on a lax name.
const testIdentity = "00000000-0000-0000-0000-000000000001"

// singleTenant pre-seeds the resolver's cache so a unit test drives one recording
// client without provisioning a database. It goes through the same map the
// production path fills, so the lookup under test is the real one.
func singleTenant(t *testing.T, client *arcadedb.Client) *tenants {
	t.Helper()
	database, err := arcadedb.DatabaseFor(testIdentity)
	if err != nil {
		t.Fatalf("DatabaseFor: %v", err)
	}
	tn := newTenants(arcadedb.Config{}, nil, nil, nil)
	tn.clients[database] = client
	return tn
}

func TestTenantsRefusesAnUnstampedCall(t *testing.T) {
	t.Parallel()
	if _, err := newTenants(arcadedb.Config{}, nil, nil, nil).For(context.Background(), ""); err == nil {
		t.Fatal("For(\"\") returned a client; memory must be per identity")
	}
}
