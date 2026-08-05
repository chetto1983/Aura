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
	return &tenants{resolver: fixedTenantClient{client: client}}
}

type fixedTenantClient struct {
	client *arcadedb.Client
}

func (f fixedTenantClient) For(_ context.Context, identityID string) (*arcadedb.Client, error) {
	if _, err := arcadedb.DatabaseFor(identityID); err != nil {
		return nil, err
	}
	return f.client, nil
}

func TestTenantsRefusesAnUnstampedCall(t *testing.T) {
	t.Parallel()
	if _, err := newTenants(arcadedb.Config{}, nil, nil, nil).For(context.Background(), ""); err == nil {
		t.Fatal("For(\"\") returned a client; memory must be per identity")
	}
}
