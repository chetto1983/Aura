package main

import (
	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/jackc/pgx/v5/pgxpool"
)

// buildFileBrowser backs the file manager's listing (GET /api/filemanager/files).
//
// It reuses the SAME per-identity resolver the asset service and document_open already go
// through, so a listing is scoped by the owner's own credential rather than by a prefix
// this code remembered to apply. A nil bundle is the pre-provisioning deployment, where
// the shared bucket is the only bucket — assets.Browser handles that case itself.
func buildFileBrowser(cfg *config.Config, pool *pgxpool.Pool, objects objectstore.Store) *assets.Browser {
	return &assets.Browser{
		Objects:      objects,
		PerIdentity:  buildObjectResolverBundle(cfg, pool),
		SharedBucket: cfg.ObjectStoreBucket,
	}
}

// buildFileOpener backs the file manager's download (GET /api/filemanager/direct) with the
// SAME resolver-gated reader document_open uses, so the two cannot disagree about whose
// bucket a key belongs to.
func buildFileOpener(cfg *config.Config, pool *pgxpool.Pool, objects objectstore.Store) identityObjectOpener {
	return identityObjectOpener{
		objects:  objects,
		resolver: buildObjectResolverBundle(cfg, pool),
		bucket:   cfg.ObjectStoreBucket,
	}
}
