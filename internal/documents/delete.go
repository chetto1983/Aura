package documents

import (
	"context"
	"fmt"
)

// DocumentGraphDeactivator marks search graph state inactive.
type DocumentGraphDeactivator interface {
	DeactivateDocument(ctx context.Context, documentID string) error
}

// DeleteService coordinates logical document delete with graph lifecycle cleanup.
type DeleteService struct {
	Catalog *CatalogService
	Graph   DocumentGraphDeactivator
}

// SoftDeleteDocument soft-deletes catalog metadata and deactivates indexed graph nodes.
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
	searchDocumentID, _ := detail.Document.Metadata["search_document_id"].(string)
	if searchDocumentID != "" && s.Graph != nil {
		if err := s.Graph.DeactivateDocument(ctx, searchDocumentID); err != nil {
			return deleted, err
		}
	}
	return deleted, nil
}
