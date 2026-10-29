package arcadedb

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// tenantOpenTimeout bounds an open that no caller is waiting for any more, so a hung server
// cannot hold a database's opens forever. The lab VM's cold open measured 2.55 s and its warm
// one 0.93 s (2026-10-29); the bound leaves room for a slower appliance, not for a hang.
const tenantOpenTimeout = 2 * time.Minute

// TenantClients resolves one identity to one server-enforced ArcadeDB boundary.
// The admin client provisions cold tenants, answers Existing's existence check and reads
// the server version; returned clients always authenticate with the credential scoped to
// their single database.
type TenantClients struct {
	base        Config
	embedder    DenseEmbedder
	admin       *Client
	credentials *TenantCredentials

	opening  singleflight.Group // one open per database, shared by the callers that arrive during it
	mu       sync.Mutex
	clients  map[string]*Client
	verified bool // the server cleared minSecureVersion; a refusal is re-checked on the next call
}

// tenantOpenLocks serializes the first open of one tenant database across every resolver in
// this process. aura serve builds one per subsystem (tenant reconcile, conversation
// projection, document index), so a gate per resolver lets several of them run the schema
// DDL on the same new database at once, and ArcadeDB does not survive that: measured on
// 2026-10-10 against 26.10.1, concurrent CREATE ... IF NOT EXISTS answered 500 "Cannot create
// type 'Entity' because already exists" and a NullPointerException in the SQL parser, and the
// boot reconcile's failure ended aura serve. Keyed by server and database; a process opens a
// bounded number of tenants, so entries are never removed. A one-slot channel rather than a
// mutex, so a caller waiting on another resolver still honors its context.
var tenantOpenSlots sync.Map

func tenantOpenSlot(baseURL, database string) chan struct{} {
	slot, _ := tenantOpenSlots.LoadOrStore(baseURL+"\x00"+database, make(chan struct{}, 1))
	return slot.(chan struct{})
}

// NewTenantClients builds a resolver without performing I/O. A nil admin means
// tenant databases must already be provisioned.
func NewTenantClients(
	base Config,
	admin *Client,
	embedder DenseEmbedder,
	credentials *TenantCredentials,
) *TenantClients {
	return &TenantClients{
		base:        base,
		embedder:    embedder,
		admin:       admin,
		credentials: credentials,
		clients:     make(map[string]*Client),
	}
}

// For returns the client for one identity, provisioning its database, scoped
// credential, and memory schema on the first successful call. The first call of a
// resolver also refuses a server older than minSecureVersion.
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

	if client, ok := t.cached(database); ok {
		return client, nil
	}
	// The open is detached from the caller that starts it: a caller that gives up stops
	// waiting, not the open. Cancelled half-way, the schema DDL restarted from its first
	// statement on every call, and a cold ArcadeDB never finished it inside the readiness
	// probe's 2 s budget (TestTenantClientsOpenOutlivesACallerThatGivesUp).
	opened := t.opening.DoChan(database, func() (any, error) {
		if client, ok := t.cached(database); ok {
			return client, nil
		}
		openCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tenantOpenTimeout)
		defer cancel()
		return t.open(openCtx, identityID, database)
	})
	select {
	case result := <-opened:
		if result.Err != nil {
			return nil, result.Err
		}
		return result.Val.(*Client), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (t *TenantClients) cached(database string) (*Client, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	client, ok := t.clients[database]
	return client, ok
}

func (t *TenantClients) open(ctx context.Context, identityID, database string) (*Client, error) {
	slot := tenantOpenSlot(t.base.BaseURL, database)
	select {
	case slot <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-slot }()

	client, err := t.client(database)
	if err != nil {
		return nil, fmt.Errorf("memory for %s: %w", identityID, err)
	}
	if err := t.verifyServer(ctx, client); err != nil {
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

// verifyServer refuses an ArcadeDB older than minSecureVersion before the first tenant
// client is handed out, and remembers a pass. It runs here rather than at boot so that a
// slow ArcadeDB start is a retried read, not a failed boot. The admin reads the version
// when there is one; otherwise the tenant's own credential does, which
// TestTenantCredentialReadsTheServerVersionLive measured to be allowed.
func (t *TenantClients) verifyServer(ctx context.Context, tenant *Client) error {
	t.mu.Lock()
	verified := t.verified
	t.mu.Unlock()
	if verified {
		return nil
	}
	server := tenant
	if t.admin != nil {
		server = t.admin
	}
	if err := server.VerifySecureVersion(ctx); err != nil {
		return err
	}
	t.mu.Lock()
	t.verified = true
	t.mu.Unlock()
	return nil
}

// Existing returns the identity's client when its memory database is already there, and
// ok=false when it is not. It never provisions: expiring, pruning and deleting have nothing
// to do in memory nobody has written, and creating the database to do nothing in it is how
// the seeded `local` operator came to own an empty one that outlived its retirement
// (measured 2026-09-11). Writers keep using For, which creates it.
//
// Existence is the admin's read, GET /api/v1/exists, not a bind as the tenant: ArcadeDB
// counts a refused bind against that user name (ServerSecurity.authenticate), and a sweep
// would add one per pass. Without an admin nothing could be provisioned anyway, so For's
// own answer stands and a missing database is an error, as it always was.
func (t *TenantClients) Existing(ctx context.Context, identityID string) (*Client, bool, error) {
	if t != nil && t.admin != nil {
		database, err := DatabaseFor(identityID)
		if err != nil {
			return nil, false, err
		}
		exists, err := t.admin.DatabaseExists(ctx, database)
		if err != nil {
			return nil, false, fmt.Errorf("memory for %s: %w", identityID, err)
		}
		if !exists {
			return nil, false, nil
		}
	}
	client, err := t.For(ctx, identityID)
	if err != nil {
		return nil, false, err
	}
	return client, true, nil
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
