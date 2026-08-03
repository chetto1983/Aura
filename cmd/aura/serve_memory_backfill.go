// serve_memory_backfill.go wires the memory_embed_backfill sweep: the scheduled caller
// that gives every fact its vector, in every identity's memory database.
package main

import (
	"context"
	"log/slog"
	"strings"

	"github.com/chetto1983/aura/internal/arcadedb"
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
	if chat == nil || chat.cfg == nil || chat.identity == nil {
		return nil
	}
	base := strings.TrimSpace(chat.cfg.ArcadeDB.BaseURL)
	if base == "" {
		slog.Warn("aura serve: no ArcadeDB server configured — memory embedding backfill disabled")
		return nil
	}
	// The same one-knob local↔cloud swap every other embedding caller takes, so the
	// sweep cannot end up writing vectors from a different model than the writes do.
	embedURL, apiKey, model := chat.cfg.EmbedRoute()
	embedder := arcadedb.NewSidecarEmbedder(embedURL, model, apiKey, 0)
	if embedder == nil {
		slog.Warn("aura serve: no embedding sidecar — memory embedding backfill disabled, retrieval stays lexical")
		return nil
	}
	credentials, err := arcadedb.NewTenantCredentials()
	if err != nil {
		slog.Warn("aura serve: no ArcadeDB tenant secret — memory embedding backfill disabled", "error", err)
		return nil
	}
	// Database is deliberately unset: the sweep selects it per identity, and a
	// default here would be a fallback that writes one tenant's vectors into
	// another's memory.
	return arcadedb.NewTenantBackfill(
		identityRoster{store: chat.identity}, arcadedb.Config{BaseURL: base}, credentials, embedder)
}
