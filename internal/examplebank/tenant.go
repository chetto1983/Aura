package examplebank

import (
	"context"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// TenantStore is Store on the identities' memory databases. A write opens the database, creating
// it if need be; a read or a forget in a database that does not exist finds nothing, because
// creating one to find nothing in it is how an empty database came to outlive its identity
// (arcadedb.TenantClients.Existing).
type TenantStore struct{ Clients *arcadedb.TenantClients }

// PutExample stores an example in the identity's database.
func (s TenantStore) PutExample(ctx context.Context, identityID string, e arcadedb.Example) error {
	client, err := s.Clients.For(ctx, identityID)
	if err != nil {
		return err
	}
	return client.PutExample(ctx, e)
}

// Examples reads a domain's examples in one space from the identity's database.
func (s TenantStore) Examples(ctx context.Context, identityID, domain, space string) ([]arcadedb.Example, error) {
	client, ok, err := s.Clients.Existing(ctx, identityID)
	if err != nil || !ok {
		return nil, err
	}
	return client.Examples(ctx, domain, space)
}

// ForgetExamples removes an owner's examples of a domain from the identity's database.
func (s TenantStore) ForgetExamples(ctx context.Context, identityID, domain, owner string) (int, error) {
	client, ok, err := s.Clients.Existing(ctx, identityID)
	if err != nil || !ok {
		return 0, err
	}
	return client.ForgetExamples(ctx, domain, owner)
}
