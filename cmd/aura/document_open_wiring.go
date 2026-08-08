package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/jackc/pgx/v5/pgxpool"
)

// runtimeDocumentOpener backs the document_open tool. It builds the object store
// and asset service per call — the same lazy shape runtimeDocumentIngestor uses
// for the opposite direction — so the tool can be registered from the shared
// buildRegistry without dragging boot-time object-store construction into every
// path that only wants a manifest.
type runtimeDocumentOpener struct {
	cfg  *config.Config
	pool *pgxpool.Pool
}

func newRuntimeDocumentOpener(cfg *config.Config, pool *pgxpool.Pool) *runtimeDocumentOpener {
	return &runtimeDocumentOpener{cfg: cfg, pool: pool}
}

func (o *runtimeDocumentOpener) OpenDocument(
	ctx context.Context,
	identityID, documentID string,
) (io.ReadCloser, documents.OpenedDocument, error) {
	if o == nil || o.cfg == nil || o.pool == nil {
		return nil, documents.OpenedDocument{}, fmt.Errorf("document opener is not configured")
	}
	objectStore, err := buildObjectStore(ctx, o.cfg)
	if err != nil {
		return nil, documents.OpenedDocument{}, fmt.Errorf("object store: %w", err)
	}
	index, err := newRuntimeDocumentIndex(o.cfg, nil, false)
	if err != nil {
		return nil, documents.OpenedDocument{}, fmt.Errorf("document index: %w", err)
	}
	service := &documents.OpenService{
		Index:   index,
		Objects: identityObjectOpener{objects: objectStore, resolver: buildObjectResolverBundle(o.cfg, o.pool), bucket: o.cfg.ObjectStoreBucket},
	}
	return service.OpenDocument(ctx, identityID, documentID)
}

// identityObjectOpener reads one object with the OWNER's own credentials.
//
// The resolver is the gate, not a courtesy: it returns the identity's own store and bucket,
// so a key belonging to somebody else cannot be read even if the caller supplied one. A nil
// resolver is the pre-provisioning deployment, where the shared store is the only store.
type identityObjectOpener struct {
	objects  objectstore.Store
	resolver *assets.ObjectResolverBundle
	bucket   string
}

func (o identityObjectOpener) OpenObject(
	ctx context.Context,
	identityID, key string,
) (io.ReadCloser, error) {
	if o.objects == nil {
		return nil, fmt.Errorf("object store is not configured")
	}
	if strings.TrimSpace(key) == "" {
		return nil, fmt.Errorf("document has no source key to open")
	}
	store, bucket := o.objects, o.bucket
	if o.resolver != nil {
		resolved, resolvedBucket, err := o.resolver.ResolveForIdentity(ctx, o.objects, identityID)
		if err != nil {
			return nil, err
		}
		store, bucket = resolved, resolvedBucket
	}
	body, _, err := store.Get(ctx, objectstore.ObjectRef{Bucket: bucket, Key: key})
	return body, err
}
