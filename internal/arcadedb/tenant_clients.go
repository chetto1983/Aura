package arcadedb

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// TenantClients resolves one identity to one server-enforced ArcadeDB boundary.
// The admin client is used only for cold provisioning; returned clients always
// authenticate with the credential scoped to their single database.
type TenantClients struct {
	base        Config
	embedder    Embedder
	admin       *Client
	credentials *TenantCredentials

	mu       sync.Mutex
	clients  map[string]*Client
	inflight map[string]chan struct{}
}

// NewTenantClients builds a resolver without performing I/O. A nil admin means
// tenant databases must already be provisioned.
func NewTenantClients(
	base Config,
	admin *Client,
	embedder Embedder,
	credentials *TenantCredentials,
) *TenantClients {
	return &TenantClients{
		base:        base,
		embedder:    embedder,
		admin:       admin,
		credentials: credentials,
		clients:     make(map[string]*Client),
		inflight:    make(map[string]chan struct{}),
	}
}

// For returns the client for one identity, provisioning its database, scoped
// credential, and memory schema on the first successful call.
func (t *TenantClients) For(ctx context.Context, identityID string) (*Client, error) {
	database, err := DatabaseFor(identityID)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, fmt.Errorf("memory for %s: tenant resolver is not configured", identityID)
	}
	if t.credentials == nil {
		return nil, fmt.Errorf("memory for %s: tenant credentials are not configured", identityID)
	}

	t.mu.Lock()
	if client, ok := t.clients[database]; ok {
		t.mu.Unlock()
		return client, nil
	}
	gate, waiting := t.inflight[database]
	if !waiting {
		gate = make(chan struct{})
		t.inflight[database] = gate
	}
	t.mu.Unlock()

	if waiting {
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

	client, err := t.client(database)
	if err != nil {
		return nil, fmt.Errorf("memory for %s: %w", identityID, err)
	}
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

func (t *TenantClients) client(database string) (*Client, error) {
	cfg := t.base
	cfg.Database = database
	cfg.User = TenantUserFor(database)
	cfg.Password = t.credentials.PasswordFor(database)
	client, err := New(cfg)
	if err != nil {
		return nil, err
	}
	return client.WithEmbedder(t.embedder), nil
}

func (t *TenantClients) provision(ctx context.Context, database string) error {
	if _, err := t.admin.CreateDatabase(ctx, database); err != nil && !tenantAlreadyExists(err) {
		return err
	}
	err := t.admin.CreateUser(ctx,
		TenantUserFor(database),
		t.credentials.PasswordFor(database),
		map[string][]string{database: {"admin"}},
	)
	if err != nil && !tenantAlreadyExists(err) {
		return err
	}
	return nil
}

func tenantAlreadyExists(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "already exists")
}
