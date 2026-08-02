package main

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// tenants caches one client per identity's database. The isolation rationale and
// the name/credential derivation live in internal/arcadedb/tenant.go, so cmd/aura
// can purge a tenant with the same mapping this sidecar provisions it with.
type tenants struct {
	base        arcadedb.Config
	embedder    arcadedb.Embedder
	admin       *arcadedb.Client
	credentials *arcadedb.TenantCredentials

	mu      sync.Mutex
	clients map[string]*arcadedb.Client
	// inflight lets concurrent first-calls for ONE tenant share a single
	// provisioning attempt without holding the lock across its I/O.
	inflight map[string]chan struct{}
}

func newTenants(
	base arcadedb.Config,
	admin *arcadedb.Client,
	embedder arcadedb.Embedder,
	credentials *arcadedb.TenantCredentials,
) *tenants {
	return &tenants{
		base:        base,
		embedder:    embedder,
		admin:       admin,
		credentials: credentials,
		clients:     map[string]*arcadedb.Client{},
		inflight:    map[string]chan struct{}{},
	}
}

// For returns the client for one identity's memory, creating the database and its
// schema the first time. The admin credential is used ONLY to create; every read
// and write afterwards goes through a client bound to that one database.
func (t *tenants) For(ctx context.Context, identityID string) (*arcadedb.Client, error) {
	database, err := arcadedb.DatabaseFor(identityID)
	if err != nil {
		return nil, err
	}
	// Only callers for the SAME tenant wait. The lock used to cover the whole
	// function, including EnsureMemorySchema's 16 DDL round trips and provisioning's
	// two more: one cold tenant blocked every other caller, cache hits included, for
	// up to the 30s client timeout each.
	t.mu.Lock()
	if client, ok := t.clients[database]; ok {
		t.mu.Unlock()
		return client, nil
	}
	gate, inflight := t.inflight[database]
	if !inflight {
		gate = make(chan struct{})
		t.inflight[database] = gate
	}
	t.mu.Unlock()
	if inflight {
		// Another caller for this same tenant is provisioning it; wait for that
		// one rather than issuing a second create.
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		t.mu.Lock()
		client, ok := t.clients[database]
		t.mu.Unlock()
		if ok {
			return client, nil
		}
		return nil, fmt.Errorf("memory for %s: provisioning failed on another call", identityID)
	}
	defer func() {
		t.mu.Lock()
		delete(t.inflight, database)
		t.mu.Unlock()
		close(gate)
	}()

	cfg := t.base
	cfg.Database = database
	if t.credentials != nil {
		// The tenant's OWN credential, derived rather than stored. It can open this
		// database and no other, so a wrong identity is refused by the server
		// instead of quietly returning someone else's memory. The username is set
		// under the same guard as the password: naming a tenant user with no password
		// to go with it would authenticate as nobody.
		cfg.User = arcadedb.TenantUserFor(database)
		cfg.Password = t.credentials.PasswordFor(database)
	}
	client, err := arcadedb.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("memory for %s: %w", identityID, err)
	}
	client = client.WithEmbedder(t.embedder)

	// EnsureMemorySchema doubles as the existence probe: it fails when the database
	// or the credential is not there yet, which is when the admin one earns its keep.
	if err := client.EnsureMemorySchema(ctx); err != nil {
		if t.admin == nil {
			return nil, fmt.Errorf("memory for %s: %w", identityID, err)
		}
		if err := t.provision(ctx, database); err != nil {
			return nil, fmt.Errorf("provision memory for %s: %w", identityID, err)
		}
		if err := client.EnsureMemorySchema(ctx); err != nil {
			return nil, fmt.Errorf("schema for %s: %w", identityID, err)
		}
	}
	t.mu.Lock()
	t.clients[database] = client
	t.mu.Unlock()
	return client, nil
}

// provision creates the database and the one credential scoped to it. Both steps
// tolerate "already exists": two concurrent first-calls for the same identity is
// a race the server resolves, not an error worth failing on.
func (t *tenants) provision(ctx context.Context, database string) error {
	if _, err := t.admin.CreateDatabase(ctx, database); err != nil && !alreadyExists(err) {
		return err
	}
	if t.credentials == nil {
		return nil
	}
	err := t.admin.CreateUser(ctx,
		arcadedb.TenantUserFor(database),
		t.credentials.PasswordFor(database),
		map[string][]string{database: {"admin"}})
	if err != nil && !alreadyExists(err) {
		return err
	}
	return nil
}

func alreadyExists(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "already exists")
}
