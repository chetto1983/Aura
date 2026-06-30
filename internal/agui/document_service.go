package agui

import (
	"context"

	"github.com/chetto1983/aura/internal/documents"
)

// DocumentCatalogService is the narrow document catalog surface consumed by AG-UI handlers.
type DocumentCatalogService interface {
	CreateDocument(context.Context, documents.CreateDocumentRequest) (documents.Document, error)
	UpdateDocument(context.Context, documents.UpdateDocumentRequest) (documents.Document, error)
	ListDocuments(context.Context, documents.ListDocumentsRequest) ([]documents.DocumentSummary, error)
	GetDocument(ctx context.Context, identityID, documentID string) (documents.DocumentDetail, error)
	DeleteDocument(ctx context.Context, identityID, documentID string) (documents.Document, error)
}

// DocumentEventService is the narrow event timeline reader consumed by document handlers.
type DocumentEventService interface {
	ListByJob(context.Context, string) ([]documents.IngestionEvent, error)
}

// SetDocumentCatalog wires the document library/catalog API.
func (s *Server) SetDocumentCatalog(service DocumentCatalogService) { s.documentCatalog = service }

// SetDocumentEvents wires document ingestion timeline reads.
func (s *Server) SetDocumentEvents(service DocumentEventService) { s.documentEvents = service }

// SetStorageOrphans wires storage orphan dry-run and cleanup APIs.
func (s *Server) SetStorageOrphans(service StorageOrphanService) { s.storageOrphans = service }
