package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/config"
)

// serve_provisioning_adaptive.go holds the adaptive-learning plane's two
// de-provisioning adapters, split out of serve_provisioning.go on touch to keep
// that file under the 600-LOC cap (the serve_provisioning_objectstore.go
// precedent).
//
// The adaptive projection lives in the SHARED ArcadeDB database, not in the
// identity's own mem_<uuid>. It is written by one background worker draining a
// global outbox, so it is scoped by an owner_id property rather than by a
// database, and dropping a tenant's memory database does not reach it. That is
// why this purge survives as an owner-scoped delete while the memory purge became
// a `drop database`.

var (
	_ agui.AdaptiveIdentityFencer = adaptiveIdentityFenceAdapter{}
	_ agui.AdaptiveGraphPurger    = adaptiveGraphPurgeAdapter{}
)

// adaptiveGraphClientOpener builds the writer for the adaptive projection.
//
// It yields a bare adaptive.GraphWriter: *arcadedb.Client satisfies that
// interface with no adapter, and unlike the Bolt-over-MCP client it replaced it
// holds no subprocess and no session, so there is nothing to close.
type adaptiveGraphClientOpener func(context.Context, *config.ArcadeDBConfig) (adaptive.GraphWriter, error)

// openAdaptiveGraphClient performs no I/O — arcadedb.New only validates — so a
// misconfigured server surfaces here rather than on the first projected event.
func openAdaptiveGraphClient(_ context.Context, cfg *config.ArcadeDBConfig) (adaptive.GraphWriter, error) {
	if cfg == nil {
		return nil, fmt.Errorf("adaptive graph client is not configured")
	}
	return arcadedb.New(arcadedb.Config{
		BaseURL:  cfg.BaseURL,
		Database: cfg.Database,
		User:     cfg.User,
		Password: cfg.Password,
	})
}

type adaptiveIdentityFenceAdapter struct {
	pool *pgxpool.Pool
}

func (a adaptiveIdentityFenceAdapter) FenceAdaptiveIdentity(ctx context.Context, identityID string) error {
	if a.pool == nil {
		return fmt.Errorf("adaptive identity fence is not configured")
	}
	var fenced bool
	err := a.pool.QueryRow(ctx,
		`SELECT aura.fence_adaptive_identity($1::uuid)`,
		identityID,
	).Scan(&fenced)
	if err != nil {
		return fmt.Errorf("fence adaptive identity %q: %w", identityID, err)
	}
	if !fenced {
		return fmt.Errorf("fence adaptive identity %q: identity not found", identityID)
	}
	return nil
}

// adaptiveGraphPurgeAdapter builds a client at execution time for the infrequent
// grace-window purge. Building it there makes a graph failure fail closed: the
// journal records adaptive_graph failed and the later identity-row step is not
// run, so the next sweep retries rather than orphaning private learning data.
type adaptiveGraphPurgeAdapter struct {
	cfg  *config.ArcadeDBConfig
	open adaptiveGraphClientOpener
}

func (a adaptiveGraphPurgeAdapter) PurgeAdaptiveGraph(ctx context.Context, identityID string) error {
	if a.cfg == nil {
		return fmt.Errorf("adaptive graph purge is not configured")
	}
	open := a.open
	if open == nil {
		open = openAdaptiveGraphClient
	}
	writer, err := open(ctx, a.cfg)
	if err != nil {
		return fmt.Errorf("open adaptive graph purge client: %w", err)
	}
	return adaptive.NewGraphStore(writer).PurgeAdaptiveGraph(ctx, identityID)
}
