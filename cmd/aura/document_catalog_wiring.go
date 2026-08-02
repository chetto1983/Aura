package main

import (
	"context"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/objectstore"
)

func buildDocumentCatalogService(chat *chatEnv) agui.DocumentCatalogService {
	return &documents.DeletingCatalog{
		Catalog: &documents.CatalogService{Store: documents.NewPostgresCatalogStore(chat.pool)},
		Assets:  runtimeDocumentAssetDeleter{assets: chat.assets},
	}
}

func buildDocumentEventService(chat *chatEnv) agui.DocumentEventService {
	return documents.NewPostgresIngestionEventStore(chat.pool)
}

func buildStorageOrphanService(chat *chatEnv, objects objectstore.Store) agui.StorageOrphanService {
	return &documents.StorageOrphanService{
		Objects: objects,
		Ledger:  documents.NewPostgresCatalogStore(chat.pool),
	}
}

type runtimeDocumentAssetDeleter struct {
	assets *assets.Service
}

func (d runtimeDocumentAssetDeleter) DeleteDocumentAsset(ctx context.Context, identityID, assetID string) error {
	if d.assets == nil {
		return nil
	}
	_, err := d.assets.Delete(ctx, identityID, assetID)
	return err
}
