package main

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
	"github.com/chetto1983/aura/internal/mcpregistry"
	"github.com/jackc/pgx/v5/pgxpool"
)

// mcp_registry.go is how every path in this binary — the daemon's board, the daemon's
// mount, and every `aura mcp` verb — reaches the ONE MCP server registry.
//
// It replaces a JSON file that had become impossible to reason about. The file was read in
// two places and written in one, so a board could list a server no write path could see;
// it was root-owned inside a container volume, so nothing outside that container could
// inspect it when it went wrong; and it went wrong — twice in one afternoon it was found
// truncated to an empty server map, taking every configured server with it, with no audit
// row and no way to tell what had written it. The registry is in Postgres now, next to the
// MCP audit trail (migration 0022) and the per-identity OAuth grants (0100).

var (
	registryOnce sync.Once
	registry     *mcpregistry.Store
	registryErr  error
)

// mcpRegistry returns the process-wide registry store, opening its pool once.
//
// One pool for the process, not one per call: the daemon's board reads this on every
// request, and a pool per request is a connection storm. A CLI verb is short-lived and
// closes with the process.
func mcpRegistry(ctx context.Context) (*mcpregistry.Store, error) {
	registryOnce.Do(func() {
		cfg := config.LoadDB()
		pool, err := db.Open(ctx, &cfg.DB)
		if err != nil {
			registryErr = fmt.Errorf("mcp registry: open database: %w", err)
			return
		}
		registry, registryErr = mcpregistry.NewStore(pool, cfg.AuthulaSecret)
	})
	return registry, registryErr
}

// loadManagedMCPConfig reads the whole registry as the ManagedConfig shape the rest of the
// binary already speaks.
//
// It takes no context and returns none of the plumbing on purpose: it is called from
// nineteen places that only ever wanted "the current servers", and every one of them used
// to also carry a filesystem path so it could write the file back. There is no path any
// more, which is the point — a caller can no longer hold a stale copy of a file and save it
// over somebody else's change.
//
// It is a var so tests can swap in an in-memory registry. That is the same seam LibreChat
// draws between its Mongo-backed ServerConfigsDB and its in-memory ServerConfigsCache: one
// interface, more than one storage. Without it every `aura mcp` unit test would need a
// live Postgres to assert its own argument parsing.
var loadManagedMCPConfig = func() (mcp.ManagedConfig, error) {
	ctx := context.Background()
	store, err := mcpRegistry(ctx)
	if err != nil {
		return mcp.ManagedConfig{}, err
	}
	return store.List(ctx)
}

// saveManagedMCPConfig applies doc and records one audit row, in a single transaction.
//
// Both halves commit or neither does. The file-based registry bought that with a
// temp→tx→rename dance (D-04); with the servers in Postgres beside the ledger it is just a
// transaction, and there is no half-written file to clean up when a process dies mid-write.
//
// A nil pool is the pool-free caller (a unit test driving a mutating function directly,
// never the real dispatch): the servers still land, unaudited, which mcpWriteManagedConfig
// has already refused under server_production before calling here.
var saveManagedMCPConfig = func(ctx context.Context, pool *pgxpool.Pool, doc mcp.ManagedConfig, in mcpmanager.MCPAuditInsert) error {
	store, err := mcpRegistry(ctx)
	if err != nil {
		return err
	}
	if pool == nil {
		return applyManagedMCPConfig(ctx, store, doc, in.ActorIdentityID)
	}
	return db.WithTx(ctx, pool, func(q *sqlc.Queries) error {
		if err := mcpmanager.InsertMCPAuditTx(ctx, q, in); err != nil {
			return err
		}
		return applyManagedMCPConfig(ctx, store.Tx(q), doc, in.ActorIdentityID)
	})
}

// applyManagedMCPConfig makes the registry match doc: every server in it is upserted, and
// any server no longer in it is removed.
//
// The callers all follow the same shape — read the whole config, change one thing, save it
// back — which is only safe because the whole document round-trips. actor is recorded on
// the rows it creates.
//
// PrepareForWrite runs first, so a malformed server or a backdoor-shaped launch declaration
// is refused here rather than persisted and discovered at the next mount.
func applyManagedMCPConfig(ctx context.Context, store *mcpregistry.Store, doc mcp.ManagedConfig, actor string) error {
	if err := mcp.PrepareForWrite(&doc); err != nil {
		return err
	}
	current, err := store.List(ctx)
	if err != nil {
		return err
	}
	for name, server := range doc.MCPServers {
		if err := store.Upsert(ctx, mcpregistry.Entry{
			Name:     name,
			Server:   server,
			Profiles: profilesFor(doc, name),
		}, actor); err != nil {
			return err
		}
	}
	for name := range current.MCPServers {
		if _, kept := doc.MCPServers[name]; kept {
			continue
		}
		if err := store.Remove(ctx, name); err != nil {
			return err
		}
	}
	return nil
}

// profilesFor collects the profiles a server belongs to. Membership rides the server's own
// row, so it is rebuilt from the document rather than stored apart from it — which is what
// used to let an install write the server and forget the profile.
func profilesFor(doc mcp.ManagedConfig, name string) []string {
	var out []string
	for profile, cfg := range doc.Profiles {
		if slices.Contains(cfg.Servers, name) {
			out = append(out, profile)
		}
	}
	return out
}

// mcpRuntimeSet resolves what this host will actually mount: the registry's servers plus
// the catalog recipes that are on by default (D-08), split into runnable configs and the
// policy map.
//
// It is read live rather than snapshotted at boot. The snapshot is what broke: config.Load
// took a copy of the registry at process start, the cockpit board served that copy, and
// every write path went to the real registry — so an operator could remove a server, get a
// success, and watch it stay on the board until a restart.
func mcpRuntimeSet() (map[string]mcp.ServerConfig, map[string]mcp.ManagedServer, error) {
	doc, err := loadManagedMCPConfig()
	if err != nil {
		return nil, nil, err
	}
	return mcpmanager.RuntimeSet(doc)
}
