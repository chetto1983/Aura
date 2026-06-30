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
