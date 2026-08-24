package main

import (
	"context"
	"errors"
	"testing"

	authulaservices "github.com/Authula/authula/services"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/agui"
)

type fakeBootstrapMemory struct {
	provisioned []string
	purged      []string
	err         error
}

func (f *fakeBootstrapMemory) ProvisionMemory(_ context.Context, identityID string) error {
	f.provisioned = append(f.provisioned, identityID)
	return f.err
}

func (f *fakeBootstrapMemory) PurgeMemory(_ context.Context, identityID string) error {
	f.purged = append(f.purged, identityID)
	return nil
}

type fakeBootstrapIdentityDeleter struct {
	deleted []string
}

func (f *fakeBootstrapIdentityDeleter) DeleteIdentity(_ context.Context, identityName string) error {
	f.deleted = append(f.deleted, identityName)
	return nil
}

func TestBootstrapMemoryFailureCompensatesCommittedTenant(t *testing.T) {
	const identityID = "44444444-4444-4444-8444-444444444444"
	const identityName = "operator@example.test"
	memory := &fakeBootstrapMemory{err: errors.New("injected: ArcadeDB unavailable")}
	identities := &fakeBootstrapIdentityDeleter{}
	service := &firstOperatorBootstrapService{memory: memory, identityDelete: identities}
	authulaCompensations := 0

	err := service.provisionTenantMemory(context.Background(), identityName, identityID, func() {
		authulaCompensations++
	})
	if err == nil {
		t.Fatal("want memory provisioning error")
	}
	if len(memory.provisioned) != 1 || memory.provisioned[0] != identityID {
		t.Fatalf("provisioned identities=%v, want [%s]", memory.provisioned, identityID)
	}
	if len(memory.purged) != 1 || memory.purged[0] != identityID {
		t.Fatalf("purged identities=%v, want [%s]", memory.purged, identityID)
	}
	if len(identities.deleted) != 1 || identities.deleted[0] != identityName {
		t.Fatalf("deleted identities=%v, want [%s]", identities.deleted, identityName)
	}
	if authulaCompensations != 1 {
		t.Fatalf("Authula compensations=%d, want 1", authulaCompensations)
	}
}

func TestWireBootstrapServiceRequiresPoolAndAuthulaProvider(t *testing.T) {
	server := &fakeBootstrapServer{}
	pool := &pgxpool.Pool{}
	provider := fakeBootstrapProvider{core: &authulaservices.CoreServices{}}
	memory := &fakeBootstrapMemory{}

	if !wireBootstrapService(server, pool, provider, memory) {
		t.Fatal("wireBootstrapService returned false with pool and Authula provider")
	}
	if server.service == nil {
		t.Fatal("SetBootstrapService was not called")
	}

	for _, tc := range []struct {
		name     string
		pool     *pgxpool.Pool
		provider bootstrapProvider
		memory   agui.MemoryProvisioner
	}{
		{name: "missing pool", provider: provider, memory: memory},
		{name: "missing provider", pool: pool, memory: memory},
		{name: "missing core", pool: pool, provider: fakeBootstrapProvider{}, memory: memory},
		{name: "missing memory provisioner", pool: pool, provider: provider},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := &fakeBootstrapServer{}
			if wireBootstrapService(server, tc.pool, tc.provider, tc.memory) {
				t.Fatal("wireBootstrapService returned true for incomplete dependencies")
			}
			if server.service != nil {
				t.Fatal("SetBootstrapService was called for incomplete dependencies")
			}
		})
	}
}

func TestWireBootstrapServiceTreatsTypedNilProviderAsMissing(t *testing.T) {
	server := &fakeBootstrapServer{}
	pool := &pgxpool.Pool{}
	var provider *panicBootstrapProvider

	if wireBootstrapService(server, pool, provider, &fakeBootstrapMemory{}) {
		t.Fatal("wireBootstrapService returned true for typed nil provider")
	}
	if server.service != nil {
		t.Fatal("SetBootstrapService was called for typed nil provider")
	}
}

type fakeBootstrapServer struct {
	service agui.BootstrapService
}

func (s *fakeBootstrapServer) SetBootstrapService(service agui.BootstrapService) {
	s.service = service
}

type fakeBootstrapProvider struct {
	core *authulaservices.CoreServices
}

func (p fakeBootstrapProvider) CoreServices() *authulaservices.CoreServices {
	return p.core
}

func (p fakeBootstrapProvider) OperatorUserID(context.Context) (string, error) {
	return "", nil
}

type panicBootstrapProvider struct{}

func (*panicBootstrapProvider) CoreServices() *authulaservices.CoreServices {
	panic("typed nil provider should be treated as missing")
}

func (*panicBootstrapProvider) OperatorUserID(context.Context) (string, error) {
	panic("typed nil provider should be treated as missing")
}
