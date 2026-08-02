package documents

import (
	"context"
	"fmt"
	"log/slog"
)

// DocumentAssetDeleter removes source assets that advertise a deleted document.
type DocumentAssetDeleter interface {
	DeleteDocumentAsset(ctx context.Context, identityID, assetID string) error
}

// DeleteService coordinates logical document delete with source-asset cleanup.
type DeleteService struct {
	Catalog *CatalogService
	Assets  DocumentAssetDeleter
}

// SoftDeleteDocument soft-deletes catalog metadata and removes the source assets.
func (s *DeleteService) SoftDeleteDocument(ctx context.Context, identityID, documentID string) (Document, error) {
	if s == nil || s.Catalog == nil {
		return Document{}, fmt.Errorf("document delete service has no catalog")
	}
	detail, err := s.Catalog.GetDocument(ctx, identityID, documentID)
	if err != nil {
		return Document{}, err
	}
	deleted, err := s.Catalog.DeleteDocument(ctx, identityID, documentID)
	if err != nil {
		return Document{}, err
	}
	// The catalog soft-delete above is the authoritative delete the operator sees.
	// Asset object cleanup is best-effort reconciliation: it must NOT fail the
	// operator's delete, because (a) the catalog step already soft-deletes the
	// document's asset rows, so re-deleting them here returns "already deleted",
	// and (b) any residue is swept by the storage-orphan tooling. A cleanup error
	// is logged and swallowed so the delete still succeeds.
	if s.Assets != nil {
		for _, version := range detail.Versions {
			if version.AssetID == "" {
				continue
			}
			if err := s.Assets.DeleteDocumentAsset(ctx, identityID, version.AssetID); err != nil {
				slog.Default().Warn("document delete: asset cleanup failed (reconciled by orphan cleanup)",
					"document_id", documentID, "asset_id", version.AssetID, "error", err)
			}
		}
	}
	return deleted, nil
}

// DeletingCatalog exposes normal catalog operations and routes delete through asset cleanup.
type DeletingCatalog struct {
	Catalog *CatalogService
	Assets  DocumentAssetDeleter
}

// CreateDocument delegates to the wrapped catalog.
func (c *DeletingCatalog) CreateDocument(ctx context.Context, req CreateDocumentRequest) (Document, error) {
	catalog, err := c.catalog()
	if err != nil {
		return Document{}, err
	}
	return catalog.CreateDocument(ctx, req)
}

// UpdateDocument delegates to the wrapped catalog.
func (c *DeletingCatalog) UpdateDocument(ctx context.Context, req UpdateDocumentRequest) (Document, error) {
	catalog, err := c.catalog()
	if err != nil {
		return Document{}, err
	}
	return catalog.UpdateDocument(ctx, req)
}

// ListDocuments delegates to the wrapped catalog.
func (c *DeletingCatalog) ListDocuments(ctx context.Context, req ListDocumentsRequest) ([]DocumentSummary, error) {
	catalog, err := c.catalog()
	if err != nil {
		return nil, err
	}
	return catalog.ListDocuments(ctx, req)
}

// GetDocument delegates to the wrapped catalog.
func (c *DeletingCatalog) GetDocument(ctx context.Context, identityID, documentID string) (DocumentDetail, error) {
	catalog, err := c.catalog()
	if err != nil {
		return DocumentDetail{}, err
	}
	return catalog.GetDocument(ctx, identityID, documentID)
}

// DeleteDocument soft-deletes the document and removes its source assets.
func (c *DeletingCatalog) DeleteDocument(ctx context.Context, identityID, documentID string) (Document, error) {
	catalog, err := c.catalog()
	if err != nil {
		return Document{}, err
	}
	return (&DeleteService{Catalog: catalog, Assets: c.Assets}).SoftDeleteDocument(ctx, identityID, documentID)
}

func (c *DeletingCatalog) catalog() (*CatalogService, error) {
	if c == nil || c.Catalog == nil {
		return nil, fmt.Errorf("deleting catalog has no catalog")
	}
	return c.Catalog, nil
}
