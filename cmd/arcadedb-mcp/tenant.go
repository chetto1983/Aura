package main

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/google/uuid"
)

// One database per identity, and the SERVER enforces it.
//
// The alternative was an owner_id column and a filter on every read. Measured on
// the live server, the difference is not stylistic:
//
//	User 'aura_memory' is not allowed to access database 'tenant_probe'
//
// That is a SecurityException from ArcadeDB. A filter is a WHERE clause that our
// code must never once forget — across search, facts-about, forget, merge,
// digest, entities and both retrieval legs — and the failure mode of forgetting
// it is one person reading another's memory, silently. A database boundary has no
// such failure mode: the credential simply cannot reach the data.
//
// It also makes deprovisioning exact. "Purge this person's memory" becomes
// `drop database`, not a sweep that has to find everything it wrote.
type tenants struct {
	base        arcadedb.Config
	embedder    arcadedb.Embedder
	admin       *arcadedb.Client
	credentials *tenantCredentials

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
	credentials *tenantCredentials,
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

// The identity is a UUID and is parsed as one. A charset pattern was tried first
// and accepted "--------" and "0-0-0-0-0": combined with an endpoint that trusts
// the asserted identity, every novel string would provision a database AND a
// server user that nothing ever removes. uuid.Parse is the same check
// serve_deprovision_purgers.go already applies before purging.

// databaseFor maps an Aura identity to its database name.
func databaseFor(identityID string) (string, error) {
	identityID = strings.TrimSpace(identityID)
	if identityID == "" {
		// Fail CLOSED. An empty identity used to mean "the shared database", which
		// is exactly the hole this closes: a caller that forgot to stamp its call
		// would read everyone's memory.
		return "", fmt.Errorf("user_identifier is required: memory is per identity")
	}
	parsed, err := uuid.Parse(identityID)
	if err != nil {
		return "", fmt.Errorf("user_identifier %q is not an identity: %w", identityID, err)
	}
	// parsed.String() is canonical lowercase 8-4-4-4-12, so the mapping is total
	// and injective: two spellings of one UUID land on one database, and two
	// UUIDs never land on the same one. `m` is not a hex digit, so no identity can
	// forge the `mem_` prefix that userFor strips.
	return "mem_" + strings.ReplaceAll(parsed.String(), "-", "_"), nil
}

// For returns the client for one identity's memory, creating the database and its
// schema the first time. The admin credential is used ONLY to create; every read
// and write afterwards goes through a client bound to that one database.
func (t *tenants) For(ctx context.Context, identityID string) (*arcadedb.Client, error) {
	database, err := databaseFor(identityID)
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
		// instead of quietly returning someone else's memory.
		cfg.User = t.credentials.userFor(database)
		cfg.Password = t.credentials.passwordFor(database)
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
		t.credentials.userFor(database),
		t.credentials.passwordFor(database),
		map[string][]string{database: {"admin"}})
	if err != nil && !alreadyExists(err) {
		return err
	}
	return nil
}

func alreadyExists(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "already exists")
}
