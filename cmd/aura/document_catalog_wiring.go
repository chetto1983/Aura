package main

import (
	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/objectstore"
)

func buildDocumentCatalogService(chat *chatEnv) agui.DocumentCatalogService {
	return &documents.DeletingCatalog{
		Catalog: &documents.CatalogService{Store: documents.NewPostgresCatalogStore(chat.pool)},
		Graph:   runtimeDocumentGraphDeactivator{cfg: chat.cfg},
	}
}

func buildStorageOrphanService(chat *chatEnv, objects objectstore.Store) agui.StorageOrphanService {
	return &documents.StorageOrphanService{
		Objects: objects,
		Ledger:  documents.NewPostgresCatalogStore(chat.pool),
	}
}
