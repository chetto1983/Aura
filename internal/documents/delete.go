package documents

import (
	"context"
	"fmt"
	"log/slog"
)

// StagedDocumentPurger removes the copy document_open wrote into the identity's
// sandbox. It is an interface so this package keeps knowing nothing about
// containers; the implementation lives at the composition root, next to the router
// document_open already holds.
type StagedDocumentPurger interface {
	PurgeStagedDocument(ctx context.Context, documentID string) error
}

// DeleteService starts durable erasure and removes staged sandbox copies immediately.
type DeleteService struct {
	Catalog *CatalogService
	Staged  StagedDocumentPurger
}

// SoftDeleteDocument moves the document to deleting and queues its verified teardown.
func (s *DeleteService) SoftDeleteDocument(ctx context.Context, identityID, documentID string) (Document, error) {
	if s == nil || s.Catalog == nil {
		return Document{}, fmt.Errorf("document delete service has no catalog")
	}
	deleting, err := s.Catalog.DeleteDocument(ctx, identityID, documentID)
	if err != nil {
		return Document{}, err
	}
	s.purgeStagedCopy(ctx, documentID)
	return deleting, nil
}

// purgeStagedCopy removes the deterministic document directory after enqueue. The
// durable worker repeats this idempotent purge and cannot finalize if it still fails.
func (s *DeleteService) purgeStagedCopy(ctx context.Context, documentID string) {
	if s.Staged == nil {
		return
	}
	if err := s.Staged.PurgeStagedDocument(ctx, documentID); err != nil {
		slog.Default().Error("document delete: staged copies are STILL READABLE in the sandbox",
			"document_id", documentID, "error", err)
	}
}

// DeletingCatalog exposes normal catalog operations and routes delete through the
// durable teardown entry point.
type DeletingCatalog struct {
	Catalog *CatalogService
	Staged  StagedDocumentPurger
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

// DeleteDocument starts the durable document teardown.
func (c *DeletingCatalog) DeleteDocument(ctx context.Context, identityID, documentID string) (Document, error) {
	catalog, err := c.catalog()
	if err != nil {
		return Document{}, err
	}
	return (&DeleteService{Catalog: catalog, Staged: c.Staged}).
		SoftDeleteDocument(ctx, identityID, documentID)
}

func (c *DeletingCatalog) catalog() (*CatalogService, error) {
	if c == nil || c.Catalog == nil {
		return nil, fmt.Errorf("deleting catalog has no catalog")
	}
	return c.Catalog, nil
}
