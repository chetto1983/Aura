package main

import (
	"context"
	"fmt"
	"io"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/documents"
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
	catalog := documents.NewPostgresCatalogStore(o.pool)
	service := &documents.OpenService{
		Catalog:  &documents.CatalogService{Store: catalog},
		Resolver: catalog,
		Assets:   identityAssetOpener{service: buildAssetService(o.cfg, o.pool, objectStore)},
	}
	return service.OpenDocument(ctx, identityID, documentID)
}

// identityAssetOpener adapts assets.Service to the narrow opener the documents
// package declares, dropping the Asset record the caller does not need. The
// ownership gate inside OpenForIdentity is the point of routing through the
// service rather than reading the object store directly.
type identityAssetOpener struct {
	service *assets.Service
}

func (a identityAssetOpener) OpenDocumentAsset(
	ctx context.Context,
	identityID, assetID string,
) (io.ReadCloser, error) {
	if a.service == nil {
		return nil, fmt.Errorf("asset service is not configured")
	}
	body, _, err := a.service.OpenForIdentity(ctx, assetID, identityID)
	return body, err
}
