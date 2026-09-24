// serve_memory_backfill.go wires the memory_embed_backfill sweep: the scheduled pass that
// keeps every memory row in the daemon's embedding space, in every identity's database.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/cron/handlers"
	"github.com/chetto1983/aura/internal/identity"
)

// identityRoster adapts the identity store onto the arcadedb.MemoryIdentities seam:
// the sweep needs the ids and nothing else, and arcadedb must not learn what an
// identity row looks like.
//
// DEACTIVATED identities are included deliberately. Their memory survives until the
// grace window closes and identity_purge drops the database, and until then it is
// still readable — so it is still worth embedding. Once the database is gone the
// tenant's credential is refused and the sweep skips it, which is the same code path
// as an identity that never stored anything.
type identityRoster struct {
	store *identity.Store
}

func (r identityRoster) IdentityIDs(ctx context.Context) ([]string, error) {
	identities, err := r.store.ListIdentities(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(identities))
	for _, id := range identities {
		if strings.TrimSpace(id.ID) != "" {
			ids = append(ids, id.ID)
		}
	}
	return ids, nil
}

// buildMemoryEmbedBackfill wires the per-tenant embedding sweep, or returns nil when
// the memory server, the tenant derivation secret or the embedding sidecar is not
// configured. Nil yields the disabled no-op sweep: a deployment with no embedder has
// no vectors to backfill, and lexical retrieval is the behaviour that shipped.
//
// The return type is the INTERFACE and every failure returns a bare nil. Returning a
// nil *arcadedb.TenantBackfill would hand the handler a non-nil interface holding a
// nil pointer, and the "disabled" branch — which tests for a nil seam — would never
// fire.
func buildMemoryEmbedBackfill(chat *chatEnv) handlers.MemoryEmbedder {
	if chat == nil {
		return nil
	}
	// The daemon's memory route, the same one every memory write uses, so the pass cannot
	// write vectors from another model than the writes do.
	if chat.memoryEmbedder == nil {
		slog.Warn("aura serve: no embedding sidecar — memory embedding backfill disabled, retrieval stays lexical")
		return nil
	}
	if walk := memoryTenantWalk(chat, "memory embedding backfill", chat.memoryEmbedder); walk != nil {
		return walk
	}
	return nil
}

// memorySweepTasks is the slice of *cron.Store the boot kick needs.
type memorySweepTasks interface {
	ListActiveTasks(ctx context.Context) ([]cron.Task, error)
	RunTaskNow(ctx context.Context, id string) error
}

// kickMemoryEmbedBackfill runs the memory pass on the scheduler's first tick. A route change
// restarts the daemon, and every memory read is lexical until the pass has run, so waiting
// out the five-minute schedule would keep memory degraded for nothing (spec §5).
func kickMemoryEmbedBackfill(ctx context.Context, tasks memorySweepTasks) error {
	active, err := tasks.ListActiveTasks(ctx)
	if err != nil {
		return fmt.Errorf("list active tasks: %w", err)
	}
	for _, task := range active {
		if task.Kind == cron.KindMemoryEmbedBackfill {
			return tasks.RunTaskNow(ctx, task.ID)
		}
	}
	return nil
}

// buildMemoryMentionLink wires the per-tenant MENTIONS-edge rebuild, or returns nil when
// the memory server or the tenant derivation secret is not configured. Nil yields the
// disabled no-op sweep. Unlike buildMemoryEmbedBackfill, this builder does NOT require an
// embedding sidecar — LinkMentions is pure text matching against facts already in memory
// (memory_backfill.go), so mention linking stays enabled even when embedding is
// unconfigured and retrieval is lexical-only.
//
// The return type is the INTERFACE and every failure returns a bare nil, for the same
// reason buildMemoryEmbedBackfill does: a nil *arcadedb.TenantBackfill boxed in a non-nil
// interface would never hit the handler's "disabled" branch.
func buildMemoryMentionLink(chat *chatEnv) handlers.MemoryMentionLinker {
	// embedder is nil deliberately: LinkMentions never reads it (unlike EmbedMissing, which
	// requires one). *arcadedb.TenantBackfill satisfies both the MemoryEmbedder and the
	// MemoryMentionLinker seam, so this builder wires only the half of its capability that
	// mention linking needs — a database and tenant credentials, no embedding sidecar.
	if walk := memoryTenantWalk(chat, "memory mention link", nil); walk != nil {
		return walk
	}
	return nil
}

// memoryTenantWalk builds the walk over every identity's memory database, or nil, logged under
// purpose, when the memory server or the tenant derivation secret is not configured.
func memoryTenantWalk(chat *chatEnv, purpose string, embedder arcadedb.DenseEmbedder) *arcadedb.TenantBackfill {
	if chat == nil || chat.cfg == nil || chat.identity == nil {
		return nil
	}
	base := strings.TrimSpace(chat.cfg.ArcadeDB.BaseURL)
	if base == "" {
		slog.Warn("aura serve: no ArcadeDB server configured — " + purpose + " disabled")
		return nil
	}
	credentials, err := arcadedb.NewTenantCredentials()
	if err != nil {
		slog.Warn("aura serve: no ArcadeDB tenant secret — "+purpose+" disabled", "error", err)
		return nil
	}
	// Database is deliberately unset: the walk selects it per identity, and a default here
	// would be a fallback that writes one tenant's vectors into another's memory.
	return arcadedb.NewTenantBackfill(identityRoster{store: chat.identity}, arcadedb.Config{BaseURL: base}, credentials, embedder)
}
